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
VERSION_CODE="${VERSION_CODE:-7}"
VERSION_NAME="${VERSION_NAME:-0.9.7}"
MIN_SDK=26
TARGET_SDK=33

# Windows (Git Bash): инструменты лежат как *.exe, а javac в classpath ждёт «;».
case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*) WIN=1; CP_SEP=';' ;;
    *) WIN=0; CP_SEP=':' ;;
esac

# tool <каталог> <имя> — путь к инструменту, на Windows с расширением .exe.
tool() {
    if [ -x "$1/$2" ]; then printf '%s' "$1/$2"; else printf '%s' "$1/$2.exe"; fi
}

AAPT2="$(tool "$BT" aapt2)"
D8="$(tool "$BT" d8)"
ZIPALIGN="$(tool "$BT" zipalign)"
JAVAC="$(tool "$JDK/bin" javac)"
JAR="$(tool "$JDK/bin" jar)"
KEYTOOL="$(tool "$JDK/bin" keytool)"
if [ -x "$BT/apksigner" ]; then
    APKSIGNER="$BT/apksigner"
elif [ -x "$BT/apksigner.bat" ]; then
    APKSIGNER="$BT/apksigner.bat"
else
    APKSIGNER="$BT/apksigner"
fi

if ! command -v zip >/dev/null 2>&1; then
    echo "не найден zip: установите его (choco install zip) или добавьте в PATH" >&2
    exit 1
fi
for path in "$AAPT2" "$D8" "$ZIPALIGN" "$APKSIGNER" "$JAVAC" "$KEYTOOL" "$AAR"; do
    if [ ! -e "$path" ]; then
        echo "не найдено: $path" >&2
        exit 1
    fi
done

rm -rf "$OUT" build
mkdir -p "$OUT" build/gen build/classes build/dex

echo "== 1/7 ресур (aapt2 compile)"
"$AAPT2" compile --dir res -o build/res.zip

echo "== 2/7 связывание (aapt2 link)"
"$AAPT2" link \
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
"$JAVAC" \
    --release 8 -encoding UTF-8 \
    -classpath "build/aar/classes.jar${CP_SEP}$PLATFORM" \
    -d build/classes \
    @build/sources.txt 2>&1 | grep -v "obsolete" || true

echo "== 4/7 dex (d8)"
"$JAR" cf build/app-classes.jar -C build/classes .
"$D8" \
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
"$ZIPALIGN" -f -p 4 "build/$NAME-unsigned.apk" "build/$NAME-aligned.apk"

echo "== 7/7 подпись (apksigner)"
KEYSTORE="${KEYSTORE:-keystore.jks}"
if [ ! -f "$KEYSTORE" ]; then
    echo "   создаю отладочный keystore"
    "$KEYTOOL" -genkeypair -keystore "$KEYSTORE" \
        -storepass posvpn -keypass posvpn -alias posvpn \
        -keyalg RSA -keysize 2048 -validity 10000 \
        -dname "CN=POSvpn Debug,O=POSvpn,C=RU"
fi
"$APKSIGNER" sign \
    --ks "$KEYSTORE" --ks-pass pass:posvpn --key-pass pass:posvpn \
    --out "$OUT/$NAME-$VERSION_NAME.apk" \
    "build/$NAME-aligned.apk"

"$APKSIGNER" verify --print-certs "$OUT/$NAME-$VERSION_NAME.apk" | head -4
echo ""
echo "Готово: $OUT/$NAME-$VERSION_NAME.apk"
ls -la "$OUT/"
