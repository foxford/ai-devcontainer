/**
 * Entry point state machine.
 *
 *   node build/state-machine/index.js   # production (built by tsup)
 *   pnpm dev                            # development (tsx watch)
 *
 * Запускается из gateway hook (см. .hermes/hermes-hooks/state-machine-launcher).
 * Защита от двойного запуска через PID-файл.
 */
import { existsSync, writeFileSync, unlinkSync, readFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'

import { log } from './log.ts'
import { runEventLoop } from './machine.ts'
import { startServer } from './server.ts'

const PID_FILE = join(homedir(), '.hermes', 'state-machine.pid')

function checkSingleton(): void {
  if (existsSync(PID_FILE)) {
    const pidStr = readFileSync(PID_FILE, 'utf8').trim()
    const pid = Number(pidStr)
    if (pid && isPidAlive(pid)) {
      log.warn('another state machine instance is already running', { pid })
      process.exit(0)
    }
  }
  writeFileSync(PID_FILE, String(process.pid))
}

function isPidAlive(pid: number): boolean {
  try {
    process.kill(pid, 0)
    return true
  } catch {
    return false
  }
}

function cleanup(): void {
  try {
    unlinkSync(PID_FILE)
  } catch {
    /* ignore */
  }
}

async function main(): Promise<void> {
  checkSingleton()
  log.info('state machine starting', {
    board: process.env.HERMES_BOARD || 'default',
    pid: process.pid,
    project_root: process.env.HERMES_PROJECT_ROOT || process.cwd(),
  })

  process.on('SIGINT', () => {
    cleanup()
    process.exit(0)
  })
  process.on('SIGTERM', () => {
    cleanup()
    process.exit(0)
  })

  startServer()
  await runEventLoop(Number(process.env.HERMES_SM_POLL_MS || 500))
}

main().catch((e: unknown) => {
  log.error('state machine crashed', {
    error: String(e),
    stack: e instanceof Error ? e.stack : undefined,
  })
  cleanup()
  process.exit(1)
})
