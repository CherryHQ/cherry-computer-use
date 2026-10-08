import { appendFileSync } from 'node:fs';
import { registryManifest, sdkManifest } from './sdk-distribution.mjs';

const sdk = sdkManifest();
const needed = !(await registryManifest(sdk.name, sdk.version));
console.log(`${sdk.name}@${sdk.version}: ${needed ? 'release required' : 'already published'}`);
if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, `needed=${needed}\n`);
