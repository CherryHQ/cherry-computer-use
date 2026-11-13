#!/usr/bin/env bash
# Read-only release validation: every runtime component matches the main App.
set -euo pipefail
app="${1:?Usage: scripts/verify-locked-use-bundle.sh APP}"
base="$app/Contents/Resources/LockedUse"
read -r -a architectures <<< "$(lipo -archs "$app/Contents/MacOS/OpenComputerUse")"
team="$(codesign -dv --verbose=4 "$app" 2>&1 | sed -n 's/^TeamIdentifier=//p')"
[[ "$team" =~ ^[A-Z0-9]{10}$ ]] || { echo 'Locked Use release requires an Apple signing team.' >&2; exit 1; }
[[ -d "$base" ]] || { echo 'Locked Use runtime is missing from the App.' >&2; exit 1; }
[[ "$(find "$base" -mindepth 1 -maxdepth 1 | wc -l | tr -d ' ')" == 4 ]] || { echo 'Unexpected Locked Use runtime components.' >&2; exit 1; }
components=(OCULockService OCULockInstaller 'Cherry Guardian.app' CherryLockAuth.bundle)
executables=(OCULockService OCULockInstaller 'Cherry Guardian.app/Contents/MacOS/OCUGuardian' 'CherryLockAuth.bundle/Contents/MacOS/CherryLockAuth')
identifiers=(com.cherryai.cherrystudio.computer-use.locked-use.broker com.cherryai.cherrystudio.computer-use.locked-use.installer com.cherryai.cherrystudio.computer-use.locked-use.guardian.dev com.cherryai.cherrystudio.computer-use.locked-use.authorization)
for index in 0 1 2 3; do
  [[ -x "$base/${executables[$index]}" ]]
  lipo "$base/${executables[$index]}" -verify_arch "${architectures[@]}"
  codesign --verify --strict -R "=identifier \"${identifiers[$index]}\" and anchor apple generic and certificate leaf[subject.OU] = \"$team\"" "$base/${components[$index]}"
  authority="$(codesign -dv --verbose=4 "$base/${components[$index]}" 2>&1 | sed -n 's/^Authority=//p' | head -n 1)"
  [[ "$authority" == 'Developer ID Application:'* ]] || { echo 'Locked Use components require Developer ID Application signatures.' >&2; exit 1; }
done
codesign --verify --deep --strict "$app"
echo "Locked Use bundle verified: ${architectures[*]}, four signed runtime components."
