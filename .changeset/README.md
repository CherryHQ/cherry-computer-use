# Package releases

Run `npm run changeset` for public SDK changes. Only `@cherrystudio/computer-use` is published. The CLI workspace is private and ignored by Changesets; native changes do not require a CLI changeset while publication is paused. Changesets opens a version PR on main; merging it publishes the SDK with `NPM_TOKEN`. See [release guide](../docs/releases/RELEASE_GUIDE.md).
