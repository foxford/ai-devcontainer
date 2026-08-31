/**
 * Минимальный structured-логгер. Пишет JSON-строки в stderr/stdout, чтобы
 * gateway hook handler видел их в его log file.
 */
type LogLevel = 'debug' | 'info' | 'warn' | 'error'

const LEVEL_ORDER: Record<LogLevel, number> = {
  debug: 10,
  error: 40,
  info: 20,
  warn: 30,
}

const MIN_LEVEL: LogLevel = (process.env.HERMES_SM_LOG_LEVEL as LogLevel) || 'info'

function emit(level: LogLevel, msg: string, fields?: Record<string, unknown>) {
  if (LEVEL_ORDER[level] < LEVEL_ORDER[MIN_LEVEL]) return
  const entry = {
    level,
    msg,
    ts: new Date().toISOString(),
    ...(fields || {}),
  }
  const out = JSON.stringify(entry)
  if (level === 'error' || level === 'warn') {
    process.stderr.write(out + '\n')
  } else {
    process.stdout.write(out + '\n')
  }
}

export const log = {
  debug: (msg: string, fields?: Record<string, unknown>) => emit('debug', msg, fields),
  error: (msg: string, fields?: Record<string, unknown>) => emit('error', msg, fields),
  info: (msg: string, fields?: Record<string, unknown>) => emit('info', msg, fields),
  warn: (msg: string, fields?: Record<string, unknown>) => emit('warn', msg, fields),
}
