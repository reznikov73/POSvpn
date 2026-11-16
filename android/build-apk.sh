#!/usr/bin/env bash
# Полная сборка POSvpn для Android: AAR (gomobile bind) + APK (build.sh).
#
# Требования:
#   - Go 1.25+ (toolchain скачается сам)
#   - JDK 17+ в $JDK (javac, jar, keytool)
#   - Android SDK: platforms/android-33, build-tools-35 в $SDK
#   - Android NDK в $ANDROID_NDK_HOME (для gomobile)
#
# Пример запуска:
#   SDK=$HOME/android-sdk JDK=$HOME/jdk ANDROID_NDK_HOME=$SDK/android-ndk-r27c ./build-apk.sh
set -euo pipefail
cd "$(dirname "$0")"

export SDK="${SDK:-$HOME/android-sdk}"
export JDK="${JDK:-$HOME/jdk}"
export ANDROID_HOME="$SDK"
export ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-$SDK/android-ndk-r27c}"
export PATH="$HOME/go/bin:$(go env GOPATH 2>/dev/null || echo ~/go)/bin:$JDK/bin:$PATH"

if ! command -v gomobile >/dev/null 2>&1; then
    echo "gomobile не найден: go install golang.org/x/mobile/cmd/gomobile@latest" >&2
    exit 1
fi

echo "== 1/2 Go-библиотека (gomobile bind)"
(
    cd go
    gomobile bind \
        -target=android/arm64,android/arm,android/amd64 \
        -androidapi 26 \
        -javapkg ru.posvpn.core \
        -o ../android/libs/posvpn.aar \
        ./mobile
)

mkdir -p android/libs
echo "== 2/2 Приложение (android/build.sh)"
(
    cd android
    ./build.sh
)

echo ""
echo "Готово. APK: android/out/POSvpn-<версия>.apk"
