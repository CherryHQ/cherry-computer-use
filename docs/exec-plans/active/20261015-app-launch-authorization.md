# Application launch authorization

- Goal: let Cherry enforce its sticky user-stop decision before any application launch.
- Scope: macOS identity lookup, SDK protocol/client, Cherry host integration; Windows/Linux launch remains unsupported.
- Contract: installed bundle identity lookup has no launch/activation effect. The host authorizes identity, then the runtime verifies it immediately before launch. Opaque runtime app IDs are not stable authorization keys.
- Verification: SDK TypeScript check and native build locally; regression cases submitted for CI, no local tests per user instruction.
- Release: patch changeset; Cherry must upgrade to the published matching SDK/native packages. Older SDKs fail closed for launch.
- Rollback: keep launch disabled if identity resolution is unavailable; never restore name-only pre-launch authorization.
