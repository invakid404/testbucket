import { spawn } from 'node:child_process'
import { relative } from 'node:path'
import {
  PreflightFailedError,
  prepareOfflineUnitEnvV1,
  runOfflineUnitChildV1,
  unitTestLauncherDefaultsV1,
  type PreparedOfflineUnitEnvV1,
  type UnitTestChildProcessV1,
} from './run-unit-tests.ts'
import { isCliMainModuleV1 } from './cli-main-module.ts'

// Discovery hard cap. `vitest list` (the naive discovery) DEADLOCKS on this
// repo's multi-project config: collecting the `unit` + `case-replset` +
// `harness-unit` projects together never terminates (each project alone lists
// in seconds; the unfiltered union hangs — 14 min with no output in CI before
// it was cancelled). Discovery below avoids `vitest list` entirely, but this
// watchdog still guarantees a fast, loud failure instead of a silent hang if
// any future discovery path stalls. Override with TB_DISCOVERY_TIMEOUT_MS.
const DISCOVERY_TIMEOUT_MS_V1 = Number(process.env.TB_DISCOVERY_TIMEOUT_MS ?? 180_000)

// Files whose repo-relative path starts with one of these prefixes are EXCLUDED
// from bucket discovery, so testbucket never buckets them. They run in the misc
// job instead: the Case Phase-2 manifest lives under `shared/f/lib/cases/` and
// is run there monolithically into one cumulative report for
// `assert_phase2_vitest_report.ts`. This exclusion is the mandel-side half of
// the coverage partition {buckets} ∪ {misc} = the full test:unit file set, with
// no overlap. Comma-separated; override with TB_DISCOVERY_EXCLUDE_PREFIXES.
const DISCOVERY_EXCLUDE_PREFIXES_V1 = (process.env.TB_DISCOVERY_EXCLUDE_PREFIXES ?? 'shared/f/lib/cases/')
  .split(',')
  .map(prefix => prefix.trim())
  .filter(Boolean)

/**
 * A child spawner that routes the child's stdout to THIS process's stderr
 * (fd 2), used only for the sealed replica-set preflight on the run path.
 *
 * The run path forwards a default-reporter log on stdout; keeping the preflight's
 * `PRECHECK_GO` line off stdout keeps that log clean for testbucket's per-bucket
 * capture. (Discovery does not use the preflight — see runDiscoveryV1.)
 */
const runPreflightToStderrV1 = ({ command, arguments: childArguments, environment }: UnitTestChildProcessV1) =>
  new Promise<number>((resolveExit, reject) => {
    const child = spawn(command, childArguments, { env: environment, stdio: ['inherit', 2, 2], shell: false })
    child.once('error', reject)
    child.once('exit', (code, signal) => {
      if (signal !== null) {
        reject(new Error(`${command} terminated by ${signal}`))
        return
      }
      resolveExit(code ?? 1)
    })
  })

/**
 * Discovery — the `<vitest-command> list --json` seam testbucket calls to learn
 * the bucketable unit set.
 *
 * testbucket buckets at FILE granularity (per-test name-slicing is deferred), so
 * it only needs the FILE LIST — yet `vitest list` imports the entire module
 * graph to enumerate test *names*, which is both expensive (~1400s cumulative
 * import) and, on this repo's 3-project config, a hang (see DISCOVERY_TIMEOUT_MS).
 *
 * So discovery uses Vitest's own no-collection file globber
 * (`globTestSpecifications()`): it resolves every project's include/exclude from
 * the SAME vitest.config.ts and returns file paths without importing a line of
 * test code. It needs no offline seal (nothing is executed) and completes in ~1s.
 * Output is the `[{name,file}]` shape testbucket's discover parses; it reduces
 * rows to unique files, so one row per file (name = repo-relative path) is exact.
 */
const runDiscoveryV1 = async (): Promise<number> => {
  const { createVitest } = await import('vitest/node')
  const vitest = await createVitest('test', { watch: false })
  try {
    const specifications = await vitest.globTestSpecifications()
    const root = process.cwd()
    const seen = new Set<string>()
    const units: Array<{ name: string; file: string }> = []
    for (const specification of specifications) {
      const file = specification.moduleId
      if (seen.has(file)) continue
      seen.add(file)
      const relativePath = relative(root, file)
      if (DISCOVERY_EXCLUDE_PREFIXES_V1.some(prefix => relativePath.startsWith(prefix))) continue
      units.push({ name: relativePath, file })
    }
    process.stdout.write(JSON.stringify(units) + '\n')
    return 0
  } finally {
    await vitest.close()
  }
}

/** Runs discovery under a hard watchdog: on stall it force-exits non-zero rather than hanging. */
const runWithDiscoveryTimeoutV1 = async (work: () => Promise<number>): Promise<number> => {
  const watchdog = setTimeout(() => {
    process.stderr.write(
      `tb-vitest: discovery exceeded ${DISCOVERY_TIMEOUT_MS_V1}ms; failing fast to avoid a silent hang\n`,
    )
    process.exit(1)
  }, DISCOVERY_TIMEOUT_MS_V1)
  try {
    return await work()
  } finally {
    clearTimeout(watchdog)
  }
}

/**
 * Serves testbucket's name-slicing Runnables call (v0.2.1+): a FILE-SCOPED
 * `list <./file> --json [--project <name>]` that testbucket appends to learn one
 * whale's per-test names before slicing it across buckets. We forward it VERBATIM
 * to `pnpm exec vitest` and let its JSON reach stdout, which testbucket parses.
 *
 * Two properties make this safe without the offline seal the `run` path needs:
 *   - It only COLLECTS one spec (imports the file to enumerate describe/it names);
 *     it runs no test bodies, no `beforeAll`, no replica set. Collection is exactly
 *     what `pnpm test:unit` already does before every run.
 *   - v0.2.1 file-scopes the list (`list <./file> …`), so it imports ONE file, not
 *     the whole project (~5–30s vs ~200s). The `./` prefix and file-before-`--json`
 *     ordering are testbucket's; we pass them through untouched.
 * We still strip real-Mongo URLs as defence, so a stray top-level connection in
 * some future whale fails fast offline instead of reaching a live database.
 */
const runRunnablesListV1 = async (
  forwardedArguments: readonly string[],
  initialEnvironment: NodeJS.ProcessEnv,
): Promise<number> => {
  const pnpm = process.platform === 'win32' ? 'pnpm.cmd' : 'pnpm'
  const environment: NodeJS.ProcessEnv = { ...initialEnvironment }
  for (const name of ['MONGO_URL', 'MONGO_URI', 'MONGODB_URL']) delete environment[name]
  // stdio 'inherit' streams vitest's clean per-test-names JSON straight to this
  // process's stdout, which is what testbucket captures from the --vitest-command.
  return await runOfflineUnitChildV1({ command: pnpm, arguments: ['exec', 'vitest', ...forwardedArguments], environment })
}

/**
 * testbucket Vitest-adapter façade — the `--vitest-command` seam.
 *
 * testbucket treats its `--vitest-command` as `program + leading args` and
 * appends its OWN subcommand. It issues THREE distinct shapes, each handled here:
 *   - `list --filesOnly --json`                  → discovery (glob, no seal): the
 *     Discover + projectFor file glob, resolved without importing test code.
 *   - `list <./file> --json [--project <name>]`  → Runnables (v0.2.1+): FILE-SCOPED
 *     per-test NAME listing for #21 name-slicing — imports ONE spec, no seal.
 *   - `run --no-file-parallelism <files> …`      → a planned bucket (offline seal)
 *
 * The two `list` shapes are told apart by `--filesOnly`: glob discovery always
 * carries it, the Runnables list never does. Glob → globTestSpecifications (no
 * import); Runnables → forwarded verbatim to real vitest (imports one file). Name
 * slicing is ENABLED (record runs `--whale-k <K>`); v0.2.1 file-scopes the
 * Runnables list so a whale's names cost ~5–30s, not the ~200s whole-project import
 * v0.2.0 paid — which is why the earlier `--whale-k 1` suppression was dropped.
 *
 * A bucket `run` is forwarded verbatim to `pnpm exec vitest` under the identical
 * strict offline seal `pnpm test:unit` uses (mongod binary resolve + strict
 * validation + replica-set preflight + CASE_REPLSET_* export, real-Mongo URLs
 * forbidden), then the owned runner temp is torn down. It must NOT hard-code `run`
 * (that is `run-unit-tests`).
 */
export const runTestbucketVitestV1 = async (
  forwardedArguments: readonly string[],
  initialEnvironment: NodeJS.ProcessEnv = process.env,
): Promise<number> => {
  if (forwardedArguments[0] === 'list') {
    // `--filesOnly` ⇒ glob discovery (no import); otherwise it is v0.2.1's
    // file-scoped name-slicing Runnables list, forwarded to real vitest.
    const work = forwardedArguments.includes('--filesOnly')
      ? runDiscoveryV1
      : () => runRunnablesListV1(forwardedArguments, initialEnvironment)
    return await runWithDiscoveryTimeoutV1(work)
  }

  let prepared: PreparedOfflineUnitEnvV1
  try {
    prepared = await prepareOfflineUnitEnvV1(initialEnvironment, {
      ...unitTestLauncherDefaultsV1,
      runChild: runPreflightToStderrV1,
    })
  } catch (error) {
    if (error instanceof PreflightFailedError) return error.exitCode
    throw error
  }
  try {
    const pnpm = process.platform === 'win32' ? 'pnpm.cmd' : 'pnpm'
    return await runOfflineUnitChildV1({
      command: pnpm,
      arguments: ['exec', 'vitest', ...forwardedArguments],
      environment: prepared.testEnvironment,
    })
  } finally {
    await prepared.cleanup()
  }
}

if (isCliMainModuleV1(import.meta.url, process.argv[1])) {
  runTestbucketVitestV1(process.argv.slice(2)).then(
    exitCode => {
      process.exitCode = exitCode
    },
    error => {
      process.stderr.write((error instanceof Error ? error.stack : String(error)) + '\n')
      process.exitCode = 1
    },
  )
}
