import path from 'node:path'

// Walk from the entry because exports maps can hide package.json.
export function findPackageRoot(entry: string, name: string, paths = path): string {
  const suffix = paths.sep + paths.join('node_modules', name)
  let directory = paths.dirname(entry)
  for (;;) {
    if (directory.endsWith(suffix)) return directory
    const parent = paths.dirname(directory)
    if (parent === directory) throw new Error(`Cannot find package root for ${name}: ${entry}`)
    directory = parent
  }
}
