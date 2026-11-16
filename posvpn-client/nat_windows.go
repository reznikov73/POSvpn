//go:build windows

// nat_windows.go — шлюз в интернет на узле Windows.
//
// Узел может отдавать свой интернет всей сети: пакеты из туннеля приходят на
// адаптер POSvpn, Windows пересылает их в физический интерфейс и подменяет
// адрес источника (NAT). Для этого нужен WinNAT — он есть в Windows
// Pro/Enterprise (компонент Hyper-V), в домашней редакции отсутствует.
//
// Объект NAT с именем POSvpn принадлежит клиенту: пока галочка включена и
// туннель поднят, объект существует; при отключении снимается. Состояние
// читается через PowerShell и разбирается как JSON, чтобы не зависеть от
// локализации сообщений.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// natObjectName — имя объекта NAT. Одно на машину, поэтому второй экземпляр
// клиента переиспользует его, а не создаёт новый.
const natObjectName = "POSvpn"

// powershellWait — предел ожидания PowerShell: запуск службы WinNAT иногда
// занимает несколько секунд.
const powershellWait = 25 * time.Second

// natInfo — что сейчас есть в системе.
type natInfo struct {
	Present bool
	Active  bool
	Prefix  string
}

// natJSON — поля Get-NetNat, которые нам нужны. PowerShell 5.1 отдаёт Active
// то как true, то как 1 — поэтому поле разбирается вручную.
type natJSON struct {
	Name   string `json:"Name"`
	Prefix string `json:"InternalIPInterfaceAddressPrefix"`
	Active any    `json:"Active"`
}

// boolValue понимает true, 1 и "true" — разные версии PowerShell сериализуют
// логическое значение по-разному.
func boolValue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "true" || s == "1"
	}
	return false
}

// natAction — что сделать с объектом NAT, чтобы привести его к нужному виду.
type natAction int

const (
	natNothing natAction = iota
	natCreate
	natRecreate
	natRemove
)

// decideNAT сравнивает текущее состояние с желаемым. Отдельная функция без
// побочных эффектов: её поведение проверяется тестом.
func decideNAT(cur natInfo, prefix string, want bool) natAction {
	if !want {
		if cur.Present {
			return natRemove
		}
		return natNothing
	}
	if !cur.Present {
		return natCreate
	}
	if cur.Prefix != prefix || !cur.Active {
		return natRecreate
	}
	return natNothing
}

// adapterNameRe — допустимое имя адаптера. Имя подставляется в сценарий
// PowerShell в одинарных кавычках, поэтому опасны только сама кавычка и
// переводы строк; скобки, кириллица и пробелы разрешены.
var adapterNameRe = regexp.MustCompile(`^[^'\r\n]{1,64}$`)

// validateNATPrefix проверяет подсеть, которую будем транслировать.
func validateNATPrefix(p netip.Prefix) (string, error) {
	if !p.IsValid() || !p.Addr().Is4() {
		return "", errors.New("NAT: нужна подсеть IPv4")
	}
	if p.Bits() < 8 || p.Bits() > 32 {
		return "", fmt.Errorf("NAT: неподходящая длина маски /%d", p.Bits())
	}
	return p.Masked().String(), nil
}

// parseNATStatus разбирает вывод Get-NetNat. Пустая строка и "null" означают,
// что объекта нет.
func parseNATStatus(out string) (natInfo, error) {
	out = strings.TrimSpace(out)
	if out == "" || out == "null" {
		return natInfo{}, nil
	}
	var raw natJSON
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return natInfo{}, fmt.Errorf("разбор состояния NAT: %w", err)
	}
	return natInfo{Present: true, Active: boolValue(raw.Active), Prefix: raw.Prefix}, nil
}

// runPowerShell выполняет сценарий и возвращает объединённый вывод.
// Окно не показывается: клиент часто работает из службы.
func runPowerShell(script string) (string, error) {
	exe := "powershell.exe"
	if root := os.Getenv("SystemRoot"); root != "" {
		candidate := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if _, err := os.Stat(candidate); err == nil {
			exe = candidate
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), powershellWait)
	defer cancel()

	cmd := exec.CommandContext(ctx, exe,
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if ctx.Err() == context.DeadlineExceeded {
		return text, errors.New("PowerShell не ответил за 25 секунд")
	}
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, errors.New(text)
	}
	return text, nil
}

// cleanPowerShellText убирает служебный префикс и лишние переводы строк.
func cleanPowerShellText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "ERR ")
	text = strings.ReplaceAll(text, "\r\n", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	return strings.TrimSpace(text)
}

// natStatus читает текущее состояние объекта NAT.
func natStatus() (natInfo, error) {
	const script = `$ErrorActionPreference='SilentlyContinue'
[Console]::OutputEncoding=[Text.Encoding]::UTF8
$n = Get-NetNat -Name 'POSvpn' | Select-Object -First 1 Name,InternalIPInterfaceAddressPrefix,Active
if ($null -eq $n) { 'null' } else { $n | ConvertTo-Json -Compress }`

	out, err := runPowerShell(script)
	if err != nil {
		return natInfo{}, fmt.Errorf("состояние NAT: %s", cleanPowerShellText(out))
	}
	return parseNATStatus(out)
}

// ensureNAT приводит NAT к нужному виду: создаёт, пересоздаёт при смене
// подсети или оставляет как есть. Повторный вызов безопасен.
func ensureNAT(prefix netip.Prefix, adapter string) error {
	want, err := validateNATPrefix(prefix)
	if err != nil {
		setNATState(false, "", err.Error())
		return err
	}
	if adapter != "" && !adapterNameRe.MatchString(adapter) {
		err := fmt.Errorf("NAT: недопустимое имя адаптера %q", adapter)
		setNATState(false, want, err.Error())
		return err
	}

	cur, err := natStatus()
	if err != nil {
		setNATState(false, want, err.Error())
		return err
	}
	if decideNAT(cur, want, true) == natNothing {
		setNATState(true, want, "")
		return nil
	}

	// Подсеть и имя адаптера прошли проверку, кавычек в них быть не может.
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'
[Console]::OutputEncoding=[Text.Encoding]::UTF8
try {
  if ((Get-Service winnat).Status -ne 'Running') { Start-Service winnat }
  if (Get-NetNat -Name '%s' -ErrorAction SilentlyContinue) { Remove-NetNat -Name '%s' -Confirm:$false }
  New-NetNat -Name '%s' -InternalIPInterfaceAddressPrefix '%s' | Out-Null
  if ('%s' -ne '') { Set-NetIPInterface -InterfaceAlias '%s' -AddressFamily IPv4 -Forwarding Enabled -ErrorAction SilentlyContinue }
  $route = (Find-NetRoute -RemoteIPAddress 8.8.8.8 | Select-Object -First 1).InterfaceAlias
  if ($route) { Set-NetIPInterface -InterfaceAlias $route -AddressFamily IPv4 -Forwarding Enabled -ErrorAction SilentlyContinue }
  'ok'
} catch { 'ERR ' + $_.Exception.Message; exit 1 }`,
		natObjectName, natObjectName, natObjectName, want, adapter, adapter)

	out, err := runPowerShell(script)
	if err != nil {
		msg := cleanPowerShellText(out)
		if msg == "" {
			msg = err.Error()
		}
		msg = "NAT не включён: " + msg
		setNATState(false, want, msg)
		return errors.New(msg)
	}
	setNATState(true, want, "")
	return nil
}

// removeNAT снимает объект NAT, если он есть.
func removeNAT() error {
	const script = `$ErrorActionPreference='Stop'
[Console]::OutputEncoding=[Text.Encoding]::UTF8
try {
  if (Get-NetNat -Name 'POSvpn' -ErrorAction SilentlyContinue) { Remove-NetNat -Name 'POSvpn' -Confirm:$false }
  'ok'
} catch { 'ERR ' + $_.Exception.Message; exit 1 }`

	out, err := runPowerShell(script)
	if err != nil {
		msg := cleanPowerShellText(out)
		if msg == "" {
			msg = err.Error()
		}
		msg = "NAT не снят: " + msg
		setNATState(false, "", msg)
		return errors.New(msg)
	}
	setNATState(false, "", "")
	return nil
}

// natState хранит последний известный результат, чтобы окно показывало
// состояние без запуска PowerShell на каждое обновление.
var natState struct {
	mu     sync.Mutex
	known  bool
	active bool
	prefix string
	err    string
}

func setNATState(active bool, prefix, errText string) {
	natState.mu.Lock()
	defer natState.mu.Unlock()
	natState.known = true
	natState.active = active
	natState.prefix = prefix
	natState.err = errText
}

// natSnapshot отдаёт последнее известное состояние NAT.
func natSnapshot() (active bool, prefix, errText string) {
	natState.mu.Lock()
	defer natState.mu.Unlock()
	return natState.active, natState.prefix, natState.err
}
