#!/usr/bin/env bash
# Сборка APK POSvpn без Gradle: aapt2 + javac + d8 + zipalign + apksigner.
#
# Требуется:
#   SDK  = каталог Android SDK с platforms/android-33 и build-tools-34
#   JDK  = JDK 17+ (javac, keytool)
#   AAR  = собранный gomobile bind файл posvpn.aar (см. ../build-apk.sh)
#
# Переменные можно передать через окружение:
#   SDK=/path/to/sdk JDK=/path/to/jdk ./build.sh
set -euo pipefail

cd "$(dirname "$0")"

SDK="${SDK:-$HOME/android-sdk}"
JDK="${JDK:-$HOME/jdk}"
BT="${BT:-$SDK/build-tools-35}"
PLATFORM="$SDK/platforms/android-33/android.jar"
AAR="libs/posvpn.aar"
OUT="out"
NAME="POSvpn"
VERSION_CODE="${VERSION_CODE:-1}"
VERSION_NAME="${VERSION_NAME:-0.9.1}"
MIN_SDK=26
TARGET_SDK=33

for tool in "$BT/aapt2" "$BT/d8" "$BT/zipalign" "$BT/apksigner" "$JDK/bin/javac" "$JDK/bin/keytool" "$AAR"; do
    if [ ! -e "$tool" ]; then
        echo "не найдено: $tool" >&2
        exit 1
    fi
done

rm -rf "$OUT" build
mkdir -p "$OUT" build/gen build/classes build/dex

echo "== 1/7 ресур (aapt2 compile)"
"$BT/aapt2" compile --dir res -o build/res.zip

echo "== 2/7 связывание (aapt2 link)"
"$BT/aapt2" link \
    -o build/base.apk \
    -I "$PLATFORM" \
    --manifest AndroidManifest.xml \
    --java build/gen \
    --auto-add-overlay \
    --min-sdk-version "$MIN_SDK" \
    --target-sdk-version "$TARGET_SDK" \
    --version-code "$VERSION_CODE" \
    --version-name "$VERSION_NAME" \
    build/res.zip

echo "== 3/7 java (javac)"
rm -rf build/aar && mkdir -p build/aar
unzip -q -o "$AAR" classes.jar -d build/aar
find build/gen -name "*.java" > build/sources.txt
find java -name "*.java" >> build/sources.txt
"$JDK/bin/javac" \
    --release 8 -encoding UTF-8 \
    -classpath "build/aar/classes.jar:$PLATFORM" \
    -d build/classes \
    @build/sources.txt 2>&1 | grep -v "obsolete" || true

echo "== 4/7 dex (d8)"
"$JDK/bin/jar" cf build/app-classes.jar -C build/classes .
"$BT/d8" \
    --release \
    --min-api "$MIN_SDK" \
    --lib "$PLATFORM" \
    --classpath build/aar/classes.jar \
    --output build/dex \
    build/app-classes.jar \
    build/aar/classes.jar

echo "== 5/7 сборка APK"
cp build/base.apk "build/$NAME-unsigned.apk"
cd build
cp -r dex/classes.dex .
mkdir -p lib
for abi in arm64-v8a armeabi-v7a x86_64; do
    mkdir -p "lib/$abi"
    unzip -q -o "../$AAR" "jni/$abi/libgojni.so" -d aar-jni
    cp "aar-jni/jni/$abi/libgojni.so" "lib/$abi/libgojni.so"
done
# Библиотеки должны быть без сжатия, чтобы Android мог их исполнять
zip -q -r "$NAME-unsigned.apk" classes.dex
zip -q -r -X "$NAME-unsigned.apk" lib
cd ..

echo "== 6/7 выравнивание (zipalign)"
"$BT/zipalign" -f -p 4 "build/$NAME-unsigned.apk" "build/$NAME-aligned.apk"

echo "== 7/7 подпись (apksigner)"
KEYSTORE="${KEYSTORE:-keystore.jks}"
if [ ! -f "$KEYSTORE" ]; then
    echo "   создаю отладочный keystore"
    "$JDK/bin/keytool" -genkeypair -keystore "$KEYSTORE" \
        -storepass posvpn -keypass posvpn -alias posvpn \
        -keyalg RSA -keysize 2048 -validity 10000 \
        -dname "CN=POSvpn Debug,O=POSvpn,C=RU"
fi
"$BT/apksigner" sign \
    --ks "$KEYSTORE" --ks-pass pass:posvpn --key-pass pass:posvpn \
    --out "$OUT/$NAME-$VERSION_NAME.apk" \
    "build/$NAME-aligned.apk"

"$BT/apksigner" verify --print-certs "$OUT/$NAME-$VERSION_NAME.apk" | head -4
echo ""
echo "Готово: $OUT/$NAME-$VERSION_NAME.apk"
ls -la "$OUT/"
