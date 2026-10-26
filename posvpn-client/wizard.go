package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

// runWizard — меню для тех, кто не хочет помнить ключи командной строки.
// Запускается, если клиент вызван без аргументов.
func runWizard() {
	in := bufio.NewReader(os.Stdin)
	for {
		cfg, _ := loadConfig()
		printSummary(cfg)
		fmt.Println("1. Создать сеть")
		fmt.Println("2. Войти в сеть")
		fmt.Println("3. Подключиться (поднять туннель)")
		fmt.Println("4. Отключиться (снять адреса и маршруты)")
		fmt.Println("5. Показать участников")
		fmt.Println("6. Выход")

		switch ask(in, "Выбор", "6") {
		case "1":
			wizardCreate(in)
		case "2":
			wizardJoin(in)
		case "3":
			wizardUp(in)
		case "4":
			if err := runDown(nil); err != nil {
				fmt.Println("Ошибка:", err)
			}
		case "5":
			if err := runStatus(nil); err != nil {
				fmt.Println("Ошибка:", err)
			}
		case "6":
			return
		default:
			fmt.Println("Нет такого пункта.")
		}
		pause(in)
	}
}

func printSummary(cfg *config) {
	fmt.Println()
	fmt.Println("=== POSvpn ===")
	if cfg == nil || cfg.Server == "" {
		fmt.Println("Сервер: не настроен")
		fmt.Println("Сеть: вход ещё не выполнялся")
		return
	}
	fmt.Printf("Сервер: %s\n", cfg.Server)
	if cfg.Token == "" {
		fmt.Println("Сеть: вход ещё не выполнялся")
	} else {
		fmt.Printf("Сеть: %s, ваш адрес %s\n", cfg.Network, cfg.IP)
	}
}

func wizardCreate(in *bufio.Reader) {
	cfg, _ := loadConfig()
	server := ask(in, "Адрес координатора", defaultIf(cfg.Server, defaultServer))
	network := ask(in, "Имя сети", "")
	if network == "" {
		fmt.Println("Имя сети обязательно.")
		return
	}
	password := askPassword(in, "Пароль сети (не короче 8 символов): ")
	if len(password) < 8 {
		fmt.Println("Пароль короче 8 символов.")
		return
	}
	node := ask(in, "Имя этого компьютера в сети", hostname())

	args := []string{"-server", server, "-network", network, "-password", password, "-node", node}
	if err := runAuth(args, true); err != nil {
		fmt.Println("Ошибка:", err)
	}
}

func wizardJoin(in *bufio.Reader) {
	cfg, _ := loadConfig()
	server := ask(in, "Адрес координатора", defaultIf(cfg.Server, defaultServer))
	network := ask(in, "Имя сети", "")
	if network == "" {
		fmt.Println("Имя сети обязательно.")
		return
	}
	password := askPassword(in, "Пароль сети: ")
	if password == "" {
		fmt.Println("Пароль пустой.")
		return
	}
	node := ask(in, "Имя этого компьютера в сети", hostname())

	args := []string{"-server", server, "-network", network, "-password", password, "-node", node}
	if err := runAuth(args, false); err != nil {
		fmt.Println("Ошибка:", err)
	}
}

func wizardUp(in *bufio.Reader) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Println("Ошибка:", err)
		return
	}
	if cfg.Token == "" {
		fmt.Println("Сначала создайте сеть или войдите в неё (пункты 1 или 2).")
		return
	}

	if path, ok := wintunPath(); !ok {
		fmt.Println("Не найден wintun.dll — драйвер виртуального адаптера.")
		fmt.Println("Скачать: .\\tools\\fetch-wintun.ps1")
		fmt.Println("Или вручную: https://www.wintun.net/ → wintun-0.14.1.zip → bin\\amd64\\wintun.dll")
		if exe, err := os.Executable(); err == nil {
			fmt.Printf("Положить рядом с exe: %s\n", filepath.Dir(exe))
		}
		return
	} else {
		fmt.Printf("Драйвер: %s\n", path)
	}

	if !isElevated() {
		fmt.Println("Для создания адаптера нужны права администратора.")
		if askYes(in, "Запустить туннель в отдельном окне от администратора?", true) {
			if err := launchElevatedUp(); err != nil {
				fmt.Println("Не удалось запустить:", err)
			} else {
				fmt.Println("Окно запущено. Подтвердите запрос UAC, если он появится.")
			}
		}
		return
	}

	fmt.Println("Туннель работает. Ctrl+C — остановить.")
	if err := runUp(nil); err != nil {
		fmt.Println("Ошибка:", err)
	}
}

// wintunPath проверяет, где лежит драйвер: рядом с exe или в System32.
func wintunPath() (string, bool) {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "wintun.dll")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
	}
	if candidate, err := filepath.Abs("wintun.dll"); err == nil {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		candidate := filepath.Join(root, "System32", "wintun.dll")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
	}
	return "", false
}

// isElevated отвечает, запущен ли процесс с правами администратора.
func isElevated() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	member, err := windows.GetCurrentProcessToken().IsMember(sid)
	return err == nil && member
}

// launchElevatedUp открывает отдельное окно с `up` от администратора и
// оставляет его открытым после завершения, чтобы было видно ошибку.
func launchElevatedUp() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	script := fmt.Sprintf(
		"Start-Process -FilePath 'cmd.exe' -ArgumentList '/k','\"%s\" up' -Verb RunAs",
		strings.ReplaceAll(exe, "'", "''"),
	)
	return exec.Command("powershell.exe", "-NoProfile", "-Command", script).Run()
}

func ask(in *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, err := in.ReadString('\n')
	if err != nil && line == "" {
		return def
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func askPassword(in *bufio.Reader, label string) string {
	fmt.Print(label)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return ""
		}
		return string(raw)
	}
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line)
}

func askYes(in *bufio.Reader, label string, def bool) bool {
	hint := "д"
	if !def {
		hint = "н"
	}
	switch strings.ToLower(ask(in, label+" (д/н)", hint)) {
	case "д", "да", "y", "yes":
		return true
	default:
		return false
	}
}

func pause(in *bufio.Reader) {
	fmt.Print("\nEnter — в меню…")
	_, _ = in.ReadString('\n')
}

func defaultIf(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
