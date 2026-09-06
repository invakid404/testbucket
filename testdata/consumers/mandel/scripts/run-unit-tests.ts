import { constants } from 'node:fs'
import { access, mkdir, mkdtemp, rm, stat } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { isAbsolute, join, resolve } from 'node:path'
import { spawn } from 'node:child_process'
import { requireStrictOfflineCaseReplsetEnvironmentV1 } from '../shared/f/lib/cases/test_support/offline_replset.ts'
import { requireCaseTemporaryPathV1 } from '../shared/f/lib/cases/test_support/temporary_paths.ts'
import { isCliMainModuleV1 } from './cli-main-module.ts'

const FORBIDDEN_MONGO_ENVIRONMENT_V1 = ['MONGO_URL', 'MONGO_URI', 'MONGODB_URL'] as const
const PREFLIGHT_SCRIPT_V1 = 'shared/f/lib/cases/test_support/replset_preflight.ts'

export type UnitTestChildProcessV1 = {
  command: string
  arguments: string[]
  environment: NodeJS.ProcessEnv
}

export type UnitTestLauncherDependenciesV1 = {
  createOwnedRunnerTemp: () => Promise<string>
  prepareMongoBinary: (downloadDir: string) => Promise<string>
  removeOwnedRunnerTemp: (path: string) => Promise<void>
  runChild: (child: UnitTestChildProcessV1) => Promise<number>
  validateMongoBinary: (path: string) => Promise<void>
}

/**
 * A prepared, strictly-sealed offline Case unit-test environment: the `env`
 * every launcher hands to `vitest`, plus a `cleanup` that removes the runner
 * temp this preparation owns (a no-op when `RUNNER_TEMP` was supplied). Both
 * `runUnitTestsV1` (which appends `run`) and the testbucket façade
 * (`scripts/tb-vitest.ts`, which forwards testbucket's own subcommand) share it,
 * so the seal is authored once.
 */
export type PreparedOfflineUnitEnvV1 = {
  testEnvironment: NodeJS.ProcessEnv
  cleanup: () => Promise<void>
}

/**
 * Thrown when the isolated replica-set preflight exits non-zero. It carries the
 * child's exit code so a launcher can surface it as its own process exit code —
 * preserving the original "return the preflight's code, skip vitest" behaviour.
 */
export class PreflightFailedError extends Error {
  readonly exitCode: number
  constructor(exitCode: number) {
    super(`Case replica-set preflight exited with code ${exitCode}`)
    this.name = 'PreflightFailedError'
    this.exitCode = exitCode
  }
}

const validateMongoBinaryV1 = async (path: string) => {
  if (!isAbsolute(path)) throw new Error('MONGOMS_SYSTEM_BINARY must be an absolute path')
  const details = await stat(path)
  if (!details.isFile()) throw new Error('MONGOMS_SYSTEM_BINARY must identify a regular file')
  await access(path, constants.X_OK)
}

const prepareMongoBinaryV1 = async (downloadDir: string) => {
  const previous = {
    MONGOMS_DOWNLOAD_DIR: process.env.MONGOMS_DOWNLOAD_DIR,
    MONGOMS_MD5_CHECK: process.env.MONGOMS_MD5_CHECK,
    MONGOMS_RUNTIME_DOWNLOAD: process.env.MONGOMS_RUNTIME_DOWNLOAD,
    MONGOMS_SYSTEM_BINARY: process.env.MONGOMS_SYSTEM_BINARY,
  }
  process.env.MONGOMS_DOWNLOAD_DIR = downloadDir
  process.env.MONGOMS_MD5_CHECK = 'true'
  process.env.MONGOMS_RUNTIME_DOWNLOAD = 'true'
  delete process.env.MONGOMS_SYSTEM_BINARY
  try {
    const { MongoBinary } = await import('mongodb-memory-server')
    return await MongoBinary.getPath({ downloadDir })
  } finally {
    for (const [name, value] of Object.entries(previous)) {
      if (value === undefined) delete process.env[name]
      else process.env[name] = value
    }
  }
}

const runChildV1 = ({ command, arguments: childArguments, environment }: UnitTestChildProcessV1) =>
  new Promise<number>((resolveExit, reject) => {
    const child = spawn(command, childArguments, { env: environment, stdio: 'inherit', shell: false })
    child.once('error', reject)
    child.once('exit', (code, signal) => {
      if (signal !== null) {
        reject(new Error(`${command} terminated by ${signal}`))
        return
      }
      resolveExit(code ?? 1)
    })
  })

const defaultsV1: UnitTestLauncherDependenciesV1 = {
  createOwnedRunnerTemp: () => mkdtemp(join(tmpdir(), 'mandel-case-unit-')),
  prepareMongoBinary: prepareMongoBinaryV1,
  removeOwnedRunnerTemp: path => rm(path, { recursive: true, force: true }),
  runChild: runChildV1,
  validateMongoBinary: validateMongoBinaryV1,
}

/**
 * Shared launcher dependency defaults, exported so alternate launchers (e.g. the
 * testbucket façade) can reuse the binary-resolve / temp / child-spawn behaviour
 * and override only what they need (e.g. where the preflight's stdout goes).
 */
export const unitTestLauncherDefaultsV1: UnitTestLauncherDependenciesV1 = defaultsV1

/** The default inherit-stdio child spawner, exported for alternate launchers. */
export const runOfflineUnitChildV1 = runChildV1

const requireRunnerTempV1 = (value: string) => {
  if (!isAbsolute(value) || resolve(value) === '/') throw new Error('RUNNER_TEMP must be an absolute non-root path')
  return resolve(value)
}

/**
 * Resolves, seals, validates, and preflights the strict offline Case unit-test
 * environment, returning the `env` for `vitest` and a `cleanup` for the runner
 * temp it owns. This is the exact preparation `pnpm test:unit` has always done —
 * extracted verbatim so the testbucket façade seals identically. It throws
 * `PreflightFailedError` (carrying the exit code) if the preflight fails, and
 * always removes any temp it created before propagating an error, so no owned
 * temp leaks on a failure path.
 */
export const prepareOfflineUnitEnvV1 = async (
  initialEnvironment: NodeJS.ProcessEnv = process.env,
  dependencies: UnitTestLauncherDependenciesV1 = defaultsV1,
): Promise<PreparedOfflineUnitEnvV1> => {
  let ownedRunnerTemp: string | undefined
  try {
    const runnerTemp = initialEnvironment.RUNNER_TEMP
      ? requireRunnerTempV1(initialEnvironment.RUNNER_TEMP)
      : requireRunnerTempV1((ownedRunnerTemp = await dependencies.createOwnedRunnerTemp()))
    const pathEnvironment = { ...initialEnvironment, RUNNER_TEMP: runnerTemp }
    const downloadDir = requireCaseTemporaryPathV1(
      initialEnvironment.MONGOMS_DOWNLOAD_DIR ?? join(runnerTemp, 'case-mongodb-binaries'),
      'MONGOMS_DOWNLOAD_DIR',
      pathEnvironment,
    )
    const replsetRoot = requireCaseTemporaryPathV1(
      initialEnvironment.EXCEPTIONS_REPLSET_TMP_ROOT ?? join(runnerTemp, 'case-replset'),
      'EXCEPTIONS_REPLSET_TMP_ROOT',
      pathEnvironment,
    )
    await Promise.all([mkdir(downloadDir, { recursive: true }), mkdir(replsetRoot, { recursive: true })])

    let systemBinary = initialEnvironment.MONGOMS_SYSTEM_BINARY
    if (systemBinary === undefined) {
      if (initialEnvironment.CI === 'true') {
        throw new Error('CI must pre-provision MONGOMS_SYSTEM_BINARY before pnpm test:unit')
      }
      systemBinary = await dependencies.prepareMongoBinary(downloadDir)
    }
    await dependencies.validateMongoBinary(systemBinary)

    const proofEnvironment: NodeJS.ProcessEnv = {
      ...initialEnvironment,
      RUNNER_TEMP: runnerTemp,
      MONGOMS_DOWNLOAD_DIR: downloadDir,
      MONGOMS_SYSTEM_BINARY: systemBinary,
      MONGOMS_RUNTIME_DOWNLOAD: 'false',
      EXCEPTIONS_REPLSET_TMP_ROOT: replsetRoot,
    }
    for (const name of FORBIDDEN_MONGO_ENVIRONMENT_V1) delete proofEnvironment[name]
    requireStrictOfflineCaseReplsetEnvironmentV1({
      RUNNER_TEMP: proofEnvironment.RUNNER_TEMP,
      MONGOMS_RUNTIME_DOWNLOAD: proofEnvironment.MONGOMS_RUNTIME_DOWNLOAD,
      MONGOMS_SYSTEM_BINARY: proofEnvironment.MONGOMS_SYSTEM_BINARY,
      MONGOMS_DOWNLOAD_DIR: proofEnvironment.MONGOMS_DOWNLOAD_DIR,
      EXCEPTIONS_REPLSET_TMP_ROOT: proofEnvironment.EXCEPTIONS_REPLSET_TMP_ROOT,
      MONGO_URL: proofEnvironment.MONGO_URL,
      MONGO_URI: proofEnvironment.MONGO_URI,
      MONGODB_URL: proofEnvironment.MONGODB_URL,
    })

    const pnpm = process.platform === 'win32' ? 'pnpm.cmd' : 'pnpm'
    const preflightExit = await dependencies.runChild({
      command: pnpm,
      arguments: ['exec', 'tsx', PREFLIGHT_SCRIPT_V1],
      environment: proofEnvironment,
    })
    if (preflightExit !== 0) throw new PreflightFailedError(preflightExit)

    const testEnvironment: NodeJS.ProcessEnv = {
      ...initialEnvironment,
      CASE_REPLSET_RUNNER_TEMP: runnerTemp,
      CASE_REPLSET_MONGOMS_DOWNLOAD_DIR: downloadDir,
      CASE_REPLSET_MONGOMS_SYSTEM_BINARY: systemBinary,
      CASE_REPLSET_TMP_ROOT: replsetRoot,
    }
    const owned = ownedRunnerTemp
    return {
      testEnvironment,
      cleanup: async () => {
        if (owned !== undefined) await dependencies.removeOwnedRunnerTemp(owned)
      },
    }
  } catch (error) {
    if (ownedRunnerTemp !== undefined) await dependencies.removeOwnedRunnerTemp(ownedRunnerTemp)
    throw error
  }
}

export const runUnitTestsV1 = async (
  argumentsV1: readonly string[],
  initialEnvironment: NodeJS.ProcessEnv = process.env,
  dependencies: UnitTestLauncherDependenciesV1 = defaultsV1,
) => {
  let prepared: PreparedOfflineUnitEnvV1
  try {
    prepared = await prepareOfflineUnitEnvV1(initialEnvironment, dependencies)
  } catch (error) {
    if (error instanceof PreflightFailedError) return error.exitCode
    throw error
  }
  try {
    const pnpm = process.platform === 'win32' ? 'pnpm.cmd' : 'pnpm'
    return await dependencies.runChild({
      command: pnpm,
      arguments: ['exec', 'vitest', 'run', ...argumentsV1],
      environment: prepared.testEnvironment,
    })
  } finally {
    await prepared.cleanup()
  }
}

if (isCliMainModuleV1(import.meta.url, process.argv[1])) {
  runUnitTestsV1(process.argv.slice(2)).then(
    exitCode => {
      process.exitCode = exitCode
    },
    error => {
      process.stderr.write((error instanceof Error ? error.stack : String(error)) + '\n')
      process.exitCode = 1
    },
  )
}
