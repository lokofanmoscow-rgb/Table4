#!/bin/sh
# Сборка Table4.exe (Windows, 64 бит). Работает и на Linux/macOS, и на Windows (Git Bash).
# Ресурсы (иконка, манифест) уже лежат в rsrc_windows_amd64.syso; пересоздать их
# после правки winres/winres.json:  go run github.com/tc-hib/go-winres@latest make --arch amd64
set -e
cd "$(dirname "$0")"
go test ./...
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-H windowsgui -s -w" -o Table4.exe .
echo "Готово: Table4.exe"
