import { resolve } from 'node:path'
import { pathToFileURL } from 'node:url'

/** URL-safe CLI guard for absolute, relative, and reserved-character paths. */
export const isCliMainModuleV1 = (moduleUrl: string, argvPath: string | undefined, cwd = process.cwd()) =>
  argvPath !== undefined && moduleUrl === pathToFileURL(resolve(cwd, argvPath)).href
