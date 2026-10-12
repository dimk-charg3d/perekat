#!/bin/sh
# Builds the Android application without Gradle or the NDK: the daemon is a
# plain Go program for arm64, and the application around it is two Java
# classes put together with the tools of the Android SDK.
set -e
cd "$(dirname "$0")"

SDK=${ANDROID_HOME:-$HOME/Android/Sdk}
TOOLS=$(ls -d "$SDK"/build-tools/* | sort -V | tail -1)
PLATFORM=$(ls -d "$SDK"/platforms/android-* | sort -V | tail -1)/android.jar
VERSION=${VERSION:-dev}
OUT=build
rm -rf "$OUT"
mkdir -p "$OUT/classes" "$OUT/gen" "$OUT/lib/arm64-v8a" "$OUT/res/mipmap-xxhdpi"

# The daemon. It is named like a library so that Android unpacks it into the
# one folder an application may run programs from.
(cd .. && make relay >/dev/null && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
    go build -tags phone -trimpath -ldflags "-s -w -X github.com/charg3d/perekat/internal/version.Version=$VERSION" -o "android/$OUT/lib/arm64-v8a/libperekat.so" ./cmd/perekat)

cp -r res/. "$OUT/res/"
magick -background none ../packaging/perekat.svg -resize 144x144 "$OUT/res/mipmap-xxhdpi/ic_launcher.png"

"$TOOLS/aapt2" compile --dir "$OUT/res" -o "$OUT/res.zip"
"$TOOLS/aapt2" link -o "$OUT/perekat-unsigned.apk" -I "$PLATFORM" --manifest AndroidManifest.xml \
    --java "$OUT/gen" --min-sdk-version 26 --target-sdk-version 34 \
    --version-code 1 --version-name "$VERSION" "$OUT/res.zip"

# The stubs let javac compile lambdas against the Android platform; d8 then
# turns them into ordinary classes.
javac -nowarn -Xlint:-options -source 8 -target 8 -bootclasspath "$PLATFORM:$TOOLS/core-lambda-stubs.jar" -d "$OUT/classes" \
    $(find src "$OUT/gen" -name '*.java')
"$TOOLS/d8" --release --lib "$PLATFORM" --min-api 26 --output "$OUT" $(find "$OUT/classes" -name '*.class')

(cd "$OUT" && "$TOOLS/aapt" add perekat-unsigned.apk classes.dex lib/arm64-v8a/libperekat.so >/dev/null)
"$TOOLS/zipalign" -f -p 4 "$OUT/perekat-unsigned.apk" "$OUT/perekat-aligned.apk"

# Signed with the debug key of this machine: fine for installing by hand, and
# updates install over each other as long as the key stays the same.
KEY=${KEYSTORE:-$HOME/.android/debug.keystore}
if [ ! -f "$KEY" ]; then
    mkdir -p "$(dirname "$KEY")"
    keytool -genkeypair -keystore "$KEY" -storepass android -keypass android -alias androiddebugkey \
        -keyalg RSA -keysize 2048 -validity 10000 -dname "CN=Android Debug,O=Android,C=US" 2>/dev/null
fi
"$TOOLS/apksigner" sign --ks "$KEY" --ks-pass pass:android --key-pass pass:android \
    --out ../bin/perekat.apk "$OUT/perekat-aligned.apk"
"$TOOLS/apksigner" verify ../bin/perekat.apk
echo "built bin/perekat.apk"
