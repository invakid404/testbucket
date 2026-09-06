import { defineConfig } from 'vitest/config'

const UNIT_TEST_TIMEOUT_MS = 30_000
const UNIT_HOOK_TIMEOUT_MS = 120_000
const CASE_REPLSET_TEST_GLOB = 'shared/f/lib/cases/**/*.replset.test.ts'

export default defineConfig({
  test: {
    projects: [
      {
        resolve: { tsconfigPaths: true },
        test: {
          name: 'unit',
          include: ['**/*.test.ts'],
          exclude: [
            '**/node_modules/**',
            '**/dist/**',
            '**/integration-tests/**',
            // Worker tests run in the workers pool, configured separately in packages/region-router/vitest.config.ts.
            'packages/region-router/**',
            // These use real one-member Mongo replica sets and run serially in
            // their own project below to keep lifecycle cleanup deterministic.
            CASE_REPLSET_TEST_GLOB,
          ],
          globals: false,
          testTimeout: UNIT_TEST_TIMEOUT_MS,
          hookTimeout: UNIT_HOOK_TIMEOUT_MS,
        },
      },
      {
        resolve: { tsconfigPaths: true },
        test: {
          name: 'case-replset',
          include: [CASE_REPLSET_TEST_GLOB],
          exclude: ['**/node_modules/**', '**/dist/**'],
          setupFiles: ['./scripts/case-replset-setup.ts'],
          globals: false,
          fileParallelism: false,
          maxWorkers: 1,
          testTimeout: UNIT_TEST_TIMEOUT_MS,
          hookTimeout: UNIT_HOOK_TIMEOUT_MS,
        },
      },
      {
        resolve: { tsconfigPaths: true },
        test: {
          // Pure (no-infra) unit tests for the purchase-order recon extraction harness helpers,
          // co-located with the harness under integration-tests/lib/.
          name: 'harness-unit',
          include: ['integration-tests/lib/purchase-order-recon-extraction/__tests__/**/*.test.ts'],
          exclude: ['**/node_modules/**', '**/dist/**'],
          globals: false,
          testTimeout: UNIT_TEST_TIMEOUT_MS,
          hookTimeout: UNIT_HOOK_TIMEOUT_MS,
        },
      },
    ],
  },
})
