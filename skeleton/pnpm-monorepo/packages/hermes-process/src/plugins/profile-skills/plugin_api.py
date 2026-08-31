"""Profile Skills: API бэкенда плагина.

Монтируется в /api/plugins/profile-skills/ дашбордом Hermes
(см. hermes_cli/web_server.py, _mount_plugin_api_routes).

Зачем
-----
Профиль Hermes (team-lead, qa, developer, ...) — это отдельный изолированный
home-каталог, созданный через `hermes profile create --clone`. Клонирование
копирует в каждый профиль весь набор установленных скиллов, поэтому у профиля
qa оказывается, например, pokemon-player. Готового UI, чтобы ограничить набор
скиллов по профилю, нет: `hermes skills config` интерактивный и меняет только
активный профиль.

Где хранится
------------
Скиллы по умолчанию включены. Список выключенных для профиля лежит в массиве
`skills.disabled` его собственного config.yaml. Это ровно то, что переключает
`hermes skills config`, и что агент читает при загрузке через
agent.skill_utils.get_disabled_skill_names (зеркало —
hermes_cli.skills_config.get_disabled_skills). Мы читаем и пишем этот массив
напрямую, по профилю, поэтому переключение не трогает кэш конфига активного
профиля у работающего веб-сервера и не меняет переменные окружения процесса.

  # ~/.hermes/profiles/qa/config.yaml
  skills:
    disabled: [pokemon-player, spotify]   # скрыто только у ЭТОГО профиля

У профиля default конфиг лежит в ~/.hermes/config.yaml; list_profiles() сам
отдаёт этот путь, так что один и тот же код работает для обоих случаев.
"""

from __future__ import annotations

import os
import sys
from pathlib import Path
from typing import Any, Dict, List

import yaml
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel

router = APIRouter()


# ---------------------------------------------------------------------------
# Хелперы обнаружения. Переиспользуем модули самого Hermes, чтобы видеть те же
# профили и скиллы, что и остальной дашборд.
# ---------------------------------------------------------------------------


def _list_profiles() -> List[Dict[str, Any]]:
    """Вернуть [{name, path, is_default}] по каждому известному профилю.

    path — это home-каталог профиля; его config.yaml лежит в
    <path>/config.yaml. Так и для дефолтного home, и для профилей в
    ~/.hermes/profiles/<name>/.
    """
    from hermes_cli import profiles as profiles_mod

    out: List[Dict[str, Any]] = []
    for info in profiles_mod.list_profiles():
        name = getattr(info, "name", "") or ""
        path = str(getattr(info, "path", "") or "")
        if not name or not path:
            continue
        out.append(
            {
                "name": name,
                "path": path,
                "is_default": bool(getattr(info, "is_default", False)),
            }
        )
    return out


def _resolve_profile(name: str) -> Dict[str, Any]:
    """Найти профиль по имени и вернуть его dict, иначе 404.

    Имя резолвится через list_profiles(), а не превращается в путь напрямую,
    поэтому путь на диске не зависит от пользовательского ввода: подсунуть
    ../ в теле запроса и выйти за пределы каталога не получится.
    """
    for profile in _list_profiles():
        if profile["name"] == name:
            return profile
    raise HTTPException(status_code=404, detail=f"Unknown profile: {name!r}")


def _all_skills() -> List[Dict[str, Any]]:
    """Вернуть все установленные скиллы, не глядя на состояние disabled."""
    from tools.skills_tool import _find_all_skills

    skills: List[Dict[str, Any]] = []
    for s in _find_all_skills(skip_disabled=True):
        name = s.get("name")
        if not name:
            continue
        skills.append(
            {
                "name": name,
                "category": s.get("category") or "",
                "description": s.get("description") or "",
            }
        )
    skills.sort(key=lambda s: (s["category"], s["name"]))
    return skills


# ---------------------------------------------------------------------------
# Чтение и запись config.yaml профиля. Трогаем только массив skills.disabled.
# ---------------------------------------------------------------------------


def _config_path(profile: Dict[str, Any]) -> Path:
    return Path(profile["path"]) / "config.yaml"


def _load_config(profile: Dict[str, Any]) -> Dict[str, Any]:
    path = _config_path(profile)
    if not path.exists():
        return {}
    try:
        data = yaml.safe_load(path.read_text(encoding="utf-8"))
    except Exception as exc:  # noqa: BLE001  (отдаём аккуратную 500)
        raise HTTPException(status_code=500, detail=f"Cannot read {path}: {exc}")
    return data if isinstance(data, dict) else {}


def _read_disabled(profile: Dict[str, Any]) -> List[str]:
    skills_cfg = _load_config(profile).get("skills")
    if not isinstance(skills_cfg, dict):
        return []
    disabled = skills_cfg.get("disabled")
    if not isinstance(disabled, list):
        return []
    return sorted({str(x) for x in disabled})


def _write_disabled(profile: Dict[str, Any], disabled: List[str]) -> List[str]:
    path = _config_path(profile)
    if not path.exists():
        raise HTTPException(status_code=404, detail=f"Profile config not found: {path}")

    data = _load_config(profile)
    skills_cfg = data.get("skills")
    if not isinstance(skills_cfg, dict):
        skills_cfg = {}
        data["skills"] = skills_cfg
    clean = sorted(set(disabled))
    skills_cfg["disabled"] = clean

    # Пишем так же, как сам Hermes сохраняет config.yaml: атомарно через
    # временный файл и rename, PyYAML. Комментарии при этом теряются (как и в
    # собственном save_config Hermes), но все ключи и значения, включая
    # шаблоны ссылок ${ENV} (мы их никогда не разворачиваем), переносятся без
    # изменений.
    try:
        from utils import atomic_yaml_write

        atomic_yaml_write(path, data, sort_keys=False)
    except Exception:  # noqa: BLE001  (запасной обычный атомарный путь записи)
        tmp = path.with_suffix(path.suffix + ".tmp")
        tmp.write_text(
            yaml.safe_dump(data, sort_keys=False, default_flow_style=False),
            encoding="utf-8",
        )
        tmp.replace(path)
    return clean


# ---------------------------------------------------------------------------
# Модели запросов
# ---------------------------------------------------------------------------


class ToggleBody(BaseModel):
    profile: str
    skill: str
    enabled: bool


class SetBody(BaseModel):
    profile: str
    disabled: List[str]


# ---------------------------------------------------------------------------
# Роуты
# ---------------------------------------------------------------------------


@router.get("/matrix")
async def matrix():
    """Вся картина профиль × скилл за один вызов.

    Отдаёт каталог скиллов и для каждого профиля его набор disabled.
    Фронт сам считает enabled = skill.name not in profile.disabled.
    """
    skills = _all_skills()
    profiles = [
        {**profile, "disabled": _read_disabled(profile)}
        for profile in _list_profiles()
    ]
    return {"skills": skills, "profiles": profiles}


@router.get("/profile/{name}")
async def profile_skills(name: str):
    """Список скиллов одного профиля, у каждого проставлен enabled."""
    profile = _resolve_profile(name)
    disabled = set(_read_disabled(profile))
    skills = _all_skills()
    for s in skills:
        s["enabled"] = s["name"] not in disabled
    return {"profile": profile, "skills": skills}


@router.post("/toggle")
async def toggle(body: ToggleBody):
    """Включить или выключить один скилл у одного профиля."""
    profile = _resolve_profile(body.profile)
    disabled = set(_read_disabled(profile))
    if body.enabled:
        disabled.discard(body.skill)
    else:
        disabled.add(body.skill)
    clean = _write_disabled(profile, sorted(disabled))
    return {
        "ok": True,
        "profile": body.profile,
        "skill": body.skill,
        "enabled": body.enabled,
        "disabled": clean,
    }


@router.post("/set")
async def set_disabled(body: SetBody):
    """Заменить весь набор disabled у профиля одним вызовом (bulk-применение)."""
    profile = _resolve_profile(body.profile)
    clean = _write_disabled(profile, body.disabled)
    return {"ok": True, "profile": body.profile, "disabled": clean}


# ---------------------------------------------------------------------------
# Побочный эффект: поднять gateway Hermes вместе с дашбордом.
#
# В Hermes нет встроенного переключателя «запусти gateway, когда стартует
# дашборд», и нет события dashboard:startup (все хук-события на стороне
# gateway). Патчить вендорный cmd_dashboard нельзя: правки снесёт hermes
# update. Поэтому опираемся на то, что этот модуль импортируется ровно один
# раз, на старте дашборда, из web_server._mount_plugin_api_routes(). Код на
# уровне импорта здесь и работает как хук на старт дашборда.
#
# Условия (должны выполняться все), чтобы это не сработало в чужом процессе:
#   * "dashboard" in sys.argv: мы внутри вызова `hermes dashboard`, а не другой
#     CLI-команды, которая просто импортнула web_server.
#   * отключение через HERMES_NO_GATEWAY_AUTOSTART=1.
#   * gateway ещё не запущен (find_gateway_pids()). Это делает запуск
#     идемпотентным и заодно защищает от рекурсии: если web_server вдруг
#     импортнётся внутри самого gateway-процесса, его pid найдётся, и будет
#     no-op.
#
# Делаем по принципу best-effort: любую ошибку глотаем, чтобы не сорвать старт
# дашборда. Спавн повторяет то, что делает сам дашборд в _spawn_hermes_action:
# тот же интерпретатор, отдельная сессия, неинтерактивный режим.
#
# Запускаем `gateway run` (документированный фоновый способ, то же самое, что
# `nohup hermes gateway run &`), а не `gateway start`. `start` отдаёт gateway
# системному супервизору (systemd/launchd/Windows task) и падает там, где его
# нет, например в WSL без systemd. Мы и так детачим через start_new_session,
# поэтому `run` переживает процесс дашборда и не требует сервис-менеджера, то
# есть работает везде, где разработчик открывает дашборд.
# ---------------------------------------------------------------------------

_GATEWAY_AUTOSTART_OPT_OUT = "HERMES_NO_GATEWAY_AUTOSTART"


def _ensure_gateway_running() -> None:
    if os.environ.get(_GATEWAY_AUTOSTART_OPT_OUT, "").lower() in {"1", "true", "yes", "on"}:
        return
    try:
        from hermes_cli.gateway import find_gateway_pids

        if find_gateway_pids():
            return  # уже запущен (или мы внутри gateway), ничего не делаем
    except Exception:
        return  # не смогли определить состояние, не рискуем плодить второй gateway

    import subprocess

    log_target: Any = subprocess.DEVNULL
    try:
        try:
            from hermes_cli.config import get_hermes_home

            log_dir = get_hermes_home() / "logs"
            log_dir.mkdir(parents=True, exist_ok=True)
            log_target = open(
                log_dir / "profile-skills-gateway-autostart.log", "ab", buffering=0
            )
        except Exception:
            log_target = subprocess.DEVNULL

        popen_kwargs: Dict[str, Any] = {
            "stdin": subprocess.DEVNULL,
            "stdout": log_target,
            "stderr": subprocess.STDOUT,
            "env": {**os.environ, "HERMES_NONINTERACTIVE": "1"},
        }
        if sys.platform == "win32":
            popen_kwargs["creationflags"] = (
                subprocess.CREATE_NEW_PROCESS_GROUP  # type: ignore[attr-defined]
                | getattr(subprocess, "DETACHED_PROCESS", 0)
            )
        else:
            popen_kwargs["start_new_session"] = True

        subprocess.Popen(
            [sys.executable, "-m", "hermes_cli.main", "gateway", "run"],
            **popen_kwargs,
        )
    except Exception:
        return
    finally:
        # Ребёнок продублировал fd, родительскую ручку можно закрыть. У DEVNULL
        # закрывать нечего: это int-заглушка, а не файловый объект.
        if hasattr(log_target, "close"):
            try:
                log_target.close()
            except Exception:
                pass


if "dashboard" in sys.argv:
    _ensure_gateway_running()
