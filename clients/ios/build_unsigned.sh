#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
python3 generate_project.py
for attempt in 1 2 3; do
  if xcodebuild -project RemoteController.xcodeproj -scheme RemoteController -configuration Release -sdk iphoneos -destination 'generic/platform=iOS' -derivedDataPath build CODE_SIGNING_ALLOWED=NO build; then
    break
  fi
  if [ "$attempt" = 3 ]; then exit 1; fi
  sleep 5
done
mkdir -p build/package/Payload
cp -R build/Build/Products/Release-iphoneos/RemoteController.app build/package/Payload/
(cd build/package && /usr/bin/zip -qry ../RemoteController-unsigned.ipa Payload)
echo 'Unsigned IPA created. Sign with your own provisioning profile before installation.'
