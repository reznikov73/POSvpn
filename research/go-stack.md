# Go-стек для Windows VPN-клиента: библиотеки, вердикты, подводные камни

Дата проверки: **26 октября 2026**. Все версии, звёзды и даты ниже получены в этот день из первоисточников
(proxy.golang.org, api.github.com, сайты проектов). Где проверить не удалось — написано «не проверено».

## 0. Главное в пяти строках

1. **Wintun — единственный разумный виртуальный адаптер для MVP**, но он **L3**: ни ARP, ни Ethernet-бродкаста.
   Это принципиально расходится с «как в Radmin VPN» (виртуальная локалка для игр).
2. **L2 остаётся только один живой открытый вариант — tap-windows6 9.27.0** (GPLv2, NDIS 6.20/6.30, последний
   релиз 19.03.2024). Цена входа: своя EV-подпись + attestation-подпись драйвера в Microsoft.
3. **wireguard-go импортируется как библиотека** (`device.NewDevice`), но WireGuard по своей модели
   (AllowedIPs) не умеет fan-out и бродкаст → «виртуальную локальную сеть» на нём не построить, только VPN-звезду.
4. **pion/ice + pion/turn + quic-go закрывают NAT traversal и релей**; ICE даёт ready-made hole punching,
   но сигналинг (обмен кандидатами) вы пишете сами через свой control-канал.
5. **GUI: Wails v3.0.0 (вышел 26.10.2026) — лучший fit** (встроенный трей, NSIS/MSIX, автостарт, Go 1.26+,
   WebView2). Fyne v2.8.2 — альтернатива, но требует C-компилятора на сборке.

---

## 1. Виртуальный адаптер

### 1.1 Что есть на рынке (проверено)

| Вариант | Уровень | Версия / дата | Статус | Вердикт |
|---|---|---|---|---|
| **Wintun** + `golang.zx2c4.com/wintun` | L3 (IPv4/IPv6) | DLL **0.14.1**, тег 17.10.2021, последний коммит 23.05.2022; Go-биндинг `v0.0.0-20230126152724-0fa3db229ce2` (26.01.2023) | живой, но заморожен | **Брать** для MVP |
| **tap-windows6** (OpenVPN TAP) | L2 (Ethernet) | **9.27.0**, релиз 19.03.2024; последний push 19.03.2024; 904★ | maintenance | Брать **только** если нужен настоящий L2 |
| Своя NDIS 6 L2-минпорт | L2 | — | — | Не брать: сертификация драйвера |
| Npcap / WinpkFilter | фильтр-драйвер, не виртуальный адаптер | не проверено в этот заход | коммерческий | Не брать |

Источники: <https://www.wintun.net/> (текст страницы: «Layer 3 TUN Driver for Windows», «precompiled and signed
versions», «the below signed DLLs are the _only supported way of distributing Wintun»),
<https://git.zx2c4.com/wintun/> (тег 0.14.1, лог: `version: bump 0.14.1`, 2021-10-17; HEAD 2022-05-23),
<https://pkg.go.dev/golang.zx2c4.com/wintun> (версия, MIT, «Published: Jan 26, 2023»),
<https://github.com/OpenVPN/tap-windows6> (9.27.0, 2024-03-19, 904★),
<https://raw.githubusercontent.com/OpenVPN/tap-windows6/master/version.m4> (`PRODUCT_VERSION 9.27.0`,
`PRODUCT_TAP_WIN_COMPONENT_ID tap0901`).

### 1.2 Wintun: что важно знать до старта

- **L3-only, по дизайну**: «a layer 3 TUN driver for Windows 7, 8, 8.1, 10, and 11… require layer 3 tunneling
  devices» (<https://git.zx2c4.com/wintun/about/>). В API пакет описан как `Pointer to layer 3 IPv4 or IPv6 packet`
  (`wintun.h`, `WINTUN_PACKET`).
- **API в Go**: `CreateAdapter(name, tunnelType, requestedGUID)`, `OpenAdapter`, `LUID()`, `StartSession(capacity)`,
  `ReceivePacket/SendPacket/AllocateSendPacket/ReleaseReceivePacket`, `ReadWaitEvent`.
  Лимиты: `PacketSizeMax = 0xffff`, `RingCapacityMin = 128 KiB`, `RingCapacityMax = 64 MiB`
  (<https://pkg.go.dev/golang.zx2c4.com/wintun>).
- **Установка — только MSI/MSM**: «MSI is the only supported method of installing Wintun; if you're using a
  different install system (such as NSIS) and cannot bother to switch to MSI/WiX, then simply bundle an embedded
  MSI that you can execute with `msiexec.exe`» (<https://git.zx2c4.com/wintun/plain/README.md>).
  Это прямо бьёт по Inno Setup: ставить Wintun придётся вложенным MSI, а не копированием DLL.
- **DLL кладётся рядом с exe**: «Install the `wintun.dll` file side-by-side with your application»
  (там же). Значит `//go:embed` + распаковка на диск при старте (LoadLibrary не грузит из памяти).
- **Админ нужен**: создание адаптера — это установка PnP-драйвера в Driver Store через SetupAPI.
  Прямой фразы «requires administrator» в документации Wintun нет (проверил `about/`, `wintun.h`, README) —
  **это вывод** из способа установки, помечаю как высокую уверенность, а не как цитату.
  Косвенно: recovery-процедура в netbird выполняется «as admin» (см. 1.4).
- **Лицензии**: исходники драйвера — GPLv2; **подписанные сборки DLL в ZIP — под более разрешительной лицензией**
  («The prebuilt signed DLLs are released under a more permissive license than GPL 2.0, included in the ZIP archive»,
  <https://www.wintun.net/>). То есть shipping официального DLL лицензионно чист, а сборка своего — уже GPLv2
  и, главное, требует своей подписи.

### 1.3 tap-windows6: цена настоящего L2

- NDIS 6.20/6.30, Win7+ (ARM64 — только NDIS 6.30) — <https://raw.githubusercontent.com/OpenVPN/tap-windows6/master/README.rst>.
- **Лицензия GPLv2** (с system-library exception для WDK; заголовочный файл dual MIT/GPLv2) —
  <https://raw.githubusercontent.com/OpenVPN/tap-windows6/master/COPYING>.
- **Сборка и подпись**: Python 2.7 + EWDK/WDK, EV-сертификат, затем **attestation-подпись через Windows Dev Center**:
  «Submit these to Windows Dev Center for attestation signing. Note that unsigned cabinet files will be
  automatically rejected». Cross-sign нужен только для Win7/8/8.1 (README.rst).
- Практический вывод: чтобы поставлять **свой** TAP, нужен EV-сертификат (~годовая плата) + Partner Center +
  HLK-прогоны; «взять драйвер OpenVPN» можно только как их бинарник с их INF/`.cat` и соблюдением GPLv2.
  Для MVP на 2–10 узлов это неоправданно.

### 1.4 Известная поломка Wintun в 2026 (важно)

NetBird (production-пользователь wintun 0.14.1) зафиксировал 20.02.2026: после обновлений Windows 11
(Insider/Dev, build 26200) запись службы драйвера протухает, `wintun.CreateAdapter()` падает с
`Error creating interface: The system cannot find the file specified`, служба `wintun` в состоянии
`STOPPED`, `WIN32_EXIT_CODE: 31 (ERROR_GEN_FAILURE)`. Лечение — `sc.exe delete wintun` + перезапуск
(драйвер переустанавливается), т.е. **в клиенте нужен auto-recovery, а не бесконечный retry**:
<https://github.com/netbirdio/netbird/issues/5408>.
Это ровно та причина, по которой «поставил DLL и забыл» не работает.

### 1.5 Если нужен именно L2-бродкаст (как в Radmin VPN)

Три честных пути:

1. **tap-windows6** — настоящий Ethernet: ARP, DHCP, IPX, широковещательные discovery-пакеты игр.
   Минусы: драйвер 2024 года, GPLv2, своя подпись, установка драйвера = UAC.
2. **Wintun + эмуляция бродкаста в userspace** (компромисс для MVP): IP-пакет на `255.255.255.255`
   или на адрес подсети — это обычный L3-пакет, его видно в TUN, и его можно размножить по всем пирам
   своего оверлея. Работает для игр/софта, которые ищут друг друга **UDP-broadcast'ом**; **не работает** для
   ARP-only discovery, IPX и всего, что требует Ethernet-кадров. Подтверждение, что L3-TUN «не даёт
   Ethernet-broadcast bridging» — README похожего проекта: <https://github.com/yhan-sun/p2wlan/blob/main/README.en.md>;
   что IP-broadcast через L3-TUN проходит — <https://github.com/danterolle/tund> (оба — вторичные источники, низкая ступень).
3. **Своя NDIS L2-минпорт** — только если продукт вырастет: месяцы работы + подпись драйвера + HLK.

Отдельно: какой драйвер использует сам Radmin VPN — **не проверял**, утверждать не берусь.

**Вердикт по п.1:** MVP — Wintun. Если Л2-бродкаст является требованием №1 ( LAN-игры) — tap-windows6,
и это отдельный, более дорогой трек. Смешивать нельзя: у адаптера либо L3, либо L2.

---

## 2. Готовые движки

| Задача | Пакет | Версия / дата | Лицензия | Вердикт |
|---|---|---|---|---|
| WireGuard в userspace как библиотека | `golang.zx2c4.com/wireguard` (`device`, `tun`, `conn`) | `v0.0.0-20261024220952-b3bc704ef0ee`, 24.10.2026; 4473★ | MIT | **Брать** (с оговорками) |
| TCP/IP-стек без ядра | `gvisor.dev/gvisor` (`pkg/tcpip/...`, `adapters/gonet`) | тегов нет, только pseudo-version `v0.0.0-20261026182257`, 26.10.2026; 19784★ | Apache-2.0 | Точечно |
| Готовый TUN на netstack | `golang.zx2c4.com/wireguard/tun/netstack` | внутри того же модуля | MIT | Для сервера/тестов |
| «Tailscale в приложении» | `tailscale.com/tsnet` | модуль `tailscale.com v1.104.2`, 20.10.2026; 38122★ | BSD-3-Clause | Прототип — да, продукт — нет |
| Внутренний движок Tailscale | `tailscale.com/wgengine` | v1.104.2 | BSD-3-Clause | **Не брать** |

### 2.1 wireguard-go как библиотека — да, можно

Проверено по исходникам (`device/device.go`, версия master на 26.10.2026):

```go
func NewDevice(tunDevice tun.Device, bind conn.Bind, logger *Logger) *Device
func (device *Device) Up() error
func (device *Device) Down() error
func (device *Device) Close()
func (device *Device) Wait() chan struct{}
```

Конфигурация — через UAPI-текст: `IpcSetOperation(r io.Reader)` / `IpcGetOperation(w io.Writer)`
(`device/uapi.go`: ключи `private_key`, `listen_port`, …). На Windows `tun.CreateTUN("wintun", mtu)` уже
сам вызывает `wintun.CreateAdapter(ifname, WintunTunnelType, requestedGUID)` и `StartSession(0x800000)`
(`tun/tun_windows.go`) — то есть связка «wireguard-go + Wintun» поддерживается апстримом.

Подводные камни:

- **Нет semver.** `golang.zx2c4.com/wireguard` отдаётся только pseudo-version (proxy: `@latest` =
  `v0.0.0-20261024220952-...`). API `device` — не контракт, ломающие изменения возможны в любой момент.
- **WireGuard — не виртуальный коммутатор.** Маршрутизация по dst-IP через AllowedIPs
  (<https://www.wireguard.com/#cryptokey-routing>: «associating public keys with a list of tunnel IP addresses
  that are allowed inside the tunnel»). Один префикс не может принадлежать двум пирам → **fan-out бродкаста
  на N узлов через AllowedIPs не делается** (вывод из модели cryptokey routing). Для «локалки» нужен свой
  слой кадров/мультикаста поверх туннеля.
- **Нет discovery**: эндпоинты, ключи, роли — ваш control-plane. Плюс keepalive/roaming настраиваются вручную.
- Один `Device` = один интерфейс; маршрутизацию между пирами и подсетями пишете вы.

### 2.2 gVisor netstack

`tun/netstack.CreateNetTUN(localAddresses, dnsServers []netip.Addr, mtu int) (tun.Device, *Net, error)` даёт
`DialTCP/ListenUDP/DialPing/LookupHost` — полноценный userspace TCP/IP без адаптера в системе.
Это удобно для **серверной стороны** (релей, координатор, тесты) и для сценариев «мой процесс — клиент сети»,
но **не даёт системе видеть интерфейс**: для системного VPN нужен настоящий TUN.

- **Тегов нет вообще**: `https://proxy.golang.org/gvisor.dev/gvisor/@v/list` вернул пустой список, только
  pseudo-versions. Значит воспроизводимость сборки — на вашей ответственности (фиксировать commit).
- Модуль большой (долгая сборка, заметный размер бинаря), производительность ниже kernel-стека,
  L2/бродкаст не поддерживается by design.

### 2.3 Tailscale: tsnet и wgengine

`tsnet` — документированный способ встроить узел: «embeds a Tailscale node directly into a Go program…
runs a fully self-contained Tailscale node inside your process using a userspace TCP/IP stack (gVisor)…
**No root privileges required**» (<https://pkg.go.dev/tailscale.com/tsnet>). Но узел входит в **tailnet**,
то есть нужен control-plane Tailscale (или Headscale) и auth-key — это не «своя сеть по имени и паролю».

`wgengine` — внутренний движок tailscaled, завязанный на `tailcfg`/`controlclient`; как публичная библиотека
не проектировался. Для продукта со своим сервером координации — **не брать**.

**Вердикт по п.2:** wireguard-go — да, если согласны на L3-звезду и отсутствие бродкаста. netstack — точечно
на сервере. tsnet/wgengine — нет (кроме быстрых прототипов и внутренних утилит).

---

## 3. NAT traversal

| Задача | Пакет | Версия / дата | Звёзды | Вердикт |
|---|---|---|---|---|
| ICE, hole punching | `github.com/pion/ice/v4` | **v4.4.8**, 19.10.2026 (v5 в RC: `v5.0.0-rc.7`, 06.10.2026) | 618 | Брать, если нужен P2P |
| STUN | `github.com/pion/stun/v3` | **v3.1.7**, 18.08.2026 (есть `v5.0.0-rc.1`) | 864 | Брать |
| TURN клиент+сервер | `github.com/pion/turn/v5` | **v5.1.3**, 24.10.2026 | 2270 | Брать для релея |
| Control-канал / релей | `github.com/quic-go/quic-go` | **v0.64.1**, 19.10.2026; `go 1.26.0` в go.mod | 11817 | Брать |

### 3.1 Что реально даёт pion/ice

- Полный ICE-агент (RFC 8445): сбор host/srflx/relay-кандидатов, **connectivity checks — это и есть hole
  punching**, роли controlling/controlled, keepalive/consent, ICE restart.
- **Чего нет**: сигналинга. Обмен SDP/candidates идёт по вашему каналу (WebSocket/QUIC к координатору).
  Нет и «политики» — когда сдаваться и уходить на TURN, решаете вы.
- Симметричный NAT/жёсткий файрвол — только релей (TURN). Для 2–10 узлов это нормально: держите TURN на своём
  сервере, он же координатор.
- **Не берите RC**: `v5.0.0-rc.7` для ice и `v5.0.0-rc.1` для stun — pre-release, API ещё ходит.

### 3.2 quic-go

- Умеет **RFC 9221 datagrams** и DPLPMTUD (<https://raw.githubusercontent.com/quic-go/quic-go/master/README.md>) —
  это то, что нужно для данных VPN: дейтаграммы не страдают от head-of-line blocking, в отличие от стримов.
- Гоняется поверх `net.PacketConn`, значит дружит с `ice.Conn` (у него есть `ReadFrom/WriteTo`).
- Требует **Go 1.26.0** (go.mod) — задаёт нижнюю границу для всего проекта.
- Подводные камни: минимум 1200 байт для QUIC-пакета (аккуратно с MTU 1420 и инкапсуляцией), свой congestion
  control не подкрутить без форка, а userspace-крипто на высоком pps ест CPU.

### 3.3 Что выбрать для MVP

Для 2–10 узлов **P2P не обязателен**: звезда через свой сервер (QUIC-релей) даёт предсказуемость и ноль
возни с NAT. ICE добавляйте вторым этапом, когда захочется убрать нагрузку с релея — код транспорта
(дейтаграммы поверх `PacketConn`) при этом не меняется.

---

## 4. Управление Windows

| Задача | Чем делать | Проверено | Вердикт |
|---|---|---|---|
| Служба | `golang.org/x/sys/windows/svc` (+ `svc/mgr`) | `func Run(name string, handler Handler) error`, `func IsWindowsService() (bool, error)` — <https://pkg.go.dev/golang.org/x/sys/windows/svc>; модуль `x/sys v0.49.0`, 06.10.2026 | Брать |
| Адреса, маршруты, DNS | `golang.zx2c4.com/wireguard/windows/tunnel/winipcfg` | `LUID.AddRoute/AddRoutes/DeleteRoute/FlushRoutes`, `LUID.SetDNS/FlushDNS/DNS`, `SetInterfaceDnsSettings` — <https://pkg.go.dev/golang.zx2c4.com/wireguard/windows/tunnel/winipcfg> | Брать |
| Fallback | `netsh interface ipv4 add address/route`, `netsh advfirewall` | практика | Только как аварийный путь |
| Брандмауэр | `netsh advfirewall firewall add rule` или COM `INetFwPolicy2` (go-ole) | не проверял API в этот заход | Брать netsh для MVP |
| Автозапуск | служба `auto` + GUI в Run-ключе | — | Брать |
| Трей | Wails v3 / Fyne (встроено), либо `fyne.io/systray v1.12.2` (09.06.2026) | <https://docs.fyne.io/explore/systray> — «Since the v2.2.0 release Fyne has built in support for a system tray menu» | Брать встроенное |

Ключевое архитектурное ограничение: **служба живёт в сессии 0 и не может показывать UI**. Значит нужен
сплит: служба (Wintun + туннель, под SYSTEM) ↔ GUI-процесс в сессии пользователя (трей, окна), общение по
именованному пайпу. Это же решает вопрос прав: GUI не требует админа, служба требует.

Практика по правам: установка/обновление службы — UAC; создание Wintun-адаптера — тоже elevation, поэтому
адаптер создаёт служба, а не GUI.

Маршруты: не забывайте про `0.0.0.0/0`-сценарии (full tunnel) — там нужен split default route через
`AddRoute` с metric и аккуратное удаление при выходе, иначе машина останется без интернета после краша.
DNS: `SetDNS` на LUID туннеля, иначе утечки через основной интерфейс; для MVP достаточно DNS-сервера
координатора.

---

## 5. GUI

| Вариант | Версия / дата | Звёзды | Требования | Вердикт |
|---|---|---|---|---|
| **Wails v3** | **v3.0.0**, 26.10.2026 (сегодня) | 36727 | Go **1.26+**, WebView2 Runtime | **Брать** |
| Wails v2 | v2.17.0, 18.10.2026 | — | Go, WebView2 | Если нужен замороженный API |
| Fyne | v2.8.2, 12.10.2026 | 28778 | Go 1.19+, **C-компилятор (cgo)**, MSYS2/MinGW на Windows | Альтернатива |
| WebView2 + свой HTTP-сервер | — | — | своя реализация окна/IPC | Не брать |

Про Wails v3 (из release notes <https://github.com/wailsapp/wails/releases/tag/v3.0.0>): «System tray. Tray icons
with menus…», «start at login», «NSIS installers and MSIX packages for Windows», «Single instance, file
associations and custom URL protocols», «requires **Go 1.26 or newer**». То есть трей, автостарт и установщик
закрыты штатно — для VPN-клиента это ровно то, что нужно.

Про Fyne (<https://docs.fyne.io/started/>): «Fyne requires 3 basic elements… the Go tools (at least version 1.19),
**a C compiler** (to connect with system graphics drivers) and a system graphics driver»; на Windows рекомендуется
MSYS2/MinGW-w64. Плюс: нет зависимости от WebView2, виджеты нативные для Fyne. Минус: cgo в CI и «не веб» UI.

WebView2: Runtime обязателен для Wails — «make sure that the WebView2 Runtime is present on the client machine.
This requirement applies to both the Evergreen and Fixed Version distribution modes»
(<https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution>). На Windows 11 он есть
из коробки, на Windows 10 — обычно есть вместе с Edge, но установщик обязан уметь доставить Evergreen
bootstrapper. Точную формулировку про «предустановлен в Win11» в этот заход не зафиксировал — **не проверено**.

**Вердикт по п.5:** Wails v3 (Go 1.26+) — меньше всего кода на трей/установщик/автостарт. Fyne — если
не хочется тащить WebView2 и устраивает cgo-сборка.

---

## 6. Сборка и поставка

| Задача | Решение | Проверено | Вердикт |
|---|---|---|---|
| Без консольного окна | `go build -ldflags "-H windowsgui -s -w"` | стандартный линкер | Брать |
| Доставка `wintun.dll` | `//go:embed` + распаковка рядом с exe | Wintun README: DLL side-by-side | Брать |
| Установка драйвера Wintun | вложенный MSI/MSM (`msiexec`) | Wintun README: «MSI is the only supported method» | Брать |
| Инсталлятор | **Inno Setup 7.1.0** (12.08.2026) или 6.7.3 (26.05.2026) | <https://jrsoftware.org/isdl.php> | Брать, но см. лицензию |
| Инсталлятор (MSI) | **WiX v6** (v3/v4/v5 «out of community support») | <https://wixtoolset.org/> | Брать, если нужен MSI/MSM |
| Подпись exe/инсталлятора | Authenticode (OV/EV) | — | Обязательно |
| Подпись драйвера | Wintun уже подписан; свой драйвер — EV + attestation в Partner Center | tap-windows6 README | Не делать свой драйвер |

Важное по инсталляторам:

- **Inno Setup теперь требует коммерческую лицензию** для коммерческого использования: «Using Inno Setup
  commercially? Please purchase a license. All commercial licenses are perpetual and require a single payment»
  (<https://jrsoftware.org/isdl.php>). Это не «freeware без ограничений», как раньше — учтите в бюджете.
- **WiX v6** — единственная поддерживаемая ветка; для Wintun это родной путь: официальный MSM встраивается
  в MSI, а «MSI is the only supported method of installing Wintun» (Wintun README).
- Если всё же Inno: кладём wintun MSI в `[Files]` и запускаем `msiexec /i wintun.msi /qn` из `[Run]`.

Подпись и SmartScreen:

- Без Authenticode любой скачанный exe/инсталлятор получает предупреждение SmartScreen; репутация набирается
  со временем и зависит от объёма загрузок. Разница OV/EV в скорости набора репутации в этот заход **не проверена** —
  не выдумываю.
- Драйвер: Wintun поставляется уже подписанным Microsoft-совместимым образом, «the below signed DLLs are the
  _only supported way of distributing Wintun» (<https://www.wintun.net/>) — свою подпись драйвера для MVP не надо.
  Для tap-windows6 своими руками: EV-сертификат + attestation-подпись, «unsigned cabinet files will be
  automatically rejected» (tap-windows6 README.rst).

---

## 7. Сводная таблица «задача → пакет → вердикт»

| Задача | Пакет / компонент | Вердикт |
|---|---|---|
| Виртуальный адаптер L3 | `golang.zx2c4.com/wintun` + `wintun.dll 0.14.1` | **Брать** |
| Виртуальный адаптер L2 | `OpenVPN/tap-windows6 9.27.0` | Брать только при жёстком требовании L2 |
| Свой NDIS-драйвер | — | Не брать |
| WireGuard-движок | `golang.zx2c4.com/wireguard/device` | **Брать** (L3, без бродкаста) |
| TCP/IP без ядра | `gvisor.dev/gvisor` + `tun/netstack` | Точечно (сервер, тесты) |
| Готовый «Tailscale в процессе» | `tailscale.com/tsnet` | Не брать для продукта |
| Внутренний движок Tailscale | `tailscale.com/wgengine` | Не брать |
| ICE / hole punching | `github.com/pion/ice/v4 v4.4.8` | Брать при P2P (не RC) |
| STUN | `github.com/pion/stun/v3 v3.1.7` | Брать |
| TURN-релей | `github.com/pion/turn/v5 v5.1.3` | Брать |
| Control-канал / релей | `github.com/quic-go/quic-go v0.64.1` | **Брать** |
| Служба Windows | `golang.org/x/sys/windows/svc` | **Брать** |
| Маршруты/адреса/DNS | `.../windows/tunnel/winipcfg` | **Брать** |
| Брандмауэр | `netsh advfirewall` / COM `INetFwPolicy2` | Брать netsh для MVP |
| Трей | Wails v3 tray / Fyne tray / `fyne.io/systray` | Брать встроенное |
| GUI | `github.com/wailsapp/wails/v3 v3.0.0` | **Брать** |
| GUI-альтернатива | `fyne.io/fyne/v2 v2.8.2` | Резерв |
| Инсталлятор | Inno Setup 7.1.0 / WiX v6 | Брать (см. лицензии) |
| Сборка | `-H windowsgui`, `//go:embed` | **Брать** |

---

## 8. Подводные камни (по убыванию боли)

1. **L3 vs L2 — главная развилка проекта.** Wintun физически не даёт Ethernet: ни ARP, ни IPX, ни
   broadcast-кадров. Если «аналог Radmin VPN» обязан поднимать LAN-игры — считайте бюджет на tap-windows6
   и подпись драйвера сразу, а не «потом». Источник: <https://git.zx2c4.com/wintun/about/> (layer 3),
   <https://pkg.go.dev/golang.zx2c4.com/wintun> (`Data *[PacketSizeMax]byte // Pointer to layer 3 IPv4 or IPv6 packet`).
2. **WireGuard не умеет fan-out.** AllowedIPs — это карта dst-IP → пир, один префикс = один пир
   (<https://www.wireguard.com/#cryptokey-routing>). Разослать один бродкаст десяти узлам через wg нельзя;
   для «локалки» нужен свой слой кадров поверх своего транспорта. Это вывод, но он следует из модели напрямую.
3. **Wintun заморожен с 2022, а Windows меняется.** 0.14.1 (17.10.2021), HEAD 23.05.2022; в 2026 зафиксированы
   протухающие записи службы драйвера после апдейтов Win11 (`ERROR_GEN_FAILURE`, «cannot find the file specified»)
   — <https://github.com/netbirdio/netbird/issues/5408>. Нужен auto-recovery: `sc delete wintun` + повторное
   создание адаптера, а не бесконечный retry.
4. **Установка Wintun — только MSI.** Попытка «скопировать DLL и заработать» на Inno/NSIS противоречит
   апстриму: «MSI is the only supported method of installing Wintun… bundle an embedded MSI… `msiexec.exe`»
   (<https://git.zx2c4.com/wintun/plain/README.md>).
5. **Права.** Создание адаптера = установка драйвера → elevation. Отсюда обязательный сплит «служба под SYSTEM +
   GUI без админа»; иначе либо UAC на каждый запуск, либо трей не работает. (Вывод; прямая цитата про admin
   в доках Wintun отсутствует.)
6. **Служба в сессии 0 не показывает UI.** Трей и окна — отдельный процесс, IPC по named pipe. Иначе получите
   «иконка есть, окна нет» на Windows 10/11.
7. **Нет semver у ключевых зависимостей.** `golang.zx2c4.com/wireguard` и `gvisor.dev/gvisor` — только
   pseudo-version (proxy `@latest` отдаёт `v0.0.0-2026...`; у gvisor список тегов пуст). Фиксируйте commit
   в `go.mod` и обновляйте осознанно.
8. **RC-ловушка pion.** `pion/ice` master уже на v5 (`v5.0.0-rc.7`), `pion/stun` — `v5.0.0-rc.1`.
   Стабильные: ice **v4.4.8**, stun **v3.1.7**. Не тяните RC в продукт.
9. **ICE не делает сигналинг.** Он делает checks (hole punching), но обмен кандидатами, аутентификацию узлов
   в сети и выбор «кто кому звонит» пишете вы. Это не «включил ICE — и P2P».
10. **Go 1.26 — фактический минимум.** quic-go `go 1.26.0` в go.mod; Wails v3 «requires Go 1.26 or newer».
    CI и разработчиков надо переводить сразу, иначе половина стека не соберётся.
11. **Inno Setup больше не бесплатен для коммерции.** «Please purchase a license» (<https://jrsoftware.org/isdl.php>).
    WiX v6 — бесплатный, но v3/v4/v5 вне поддержки, миграция старых примеров не пройдёт.
12. **WebView2 надо доставлять.** На Win10 Runtime может отсутствовать; установщик должен уметь Evergreen
    bootstrapper, иначе «белое окно» на первом запуске (<https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution>).
13. **Fyne тянет cgo.** «a C compiler (to connect with system graphics drivers)» (<https://docs.fyne.io/started/>):
    кросс-компиляция и CI усложняются; зато нет WebView2.
14. **GPLv2 у tap-windows6.** Свой продукт с их драйвером — это GPL-обязательства (исходники, предложение
    исходников). Юридически чище свой драйвер + своя подпись, но это дороже.
15. **MTU и фрагментация.** TUN 1420 + QUIC (min 1200) + возможная инкапсуляция → чёрные дыры на PMTU.
    Закладывайте DPLPMTUD у quic-go и тесты на 1280.

---

## 9. Что не проверено (и как проверять)

- Точная формулировка Microsoft «WebView2 Runtime предустановлен в Windows 11» — страница дистрибуции в этот
  заход отдала только требование наличия Runtime. Проверить: `learn.microsoft.com/microsoft-edge/webview2/`
  раздел «Evergreen vs Fixed Version».
- Разница OV/EV для скорости набора репутации SmartScreen — публичных первичных данных не смотрел.
- Какой драйвер использует Radmin VPN (L2-минпорт или TAP-производный) — не проверял; для архитектуры это
  не требуется, требование сформулировано как «нужен L2-бродкаст».
- Npcap/WinpkFilter как альтернатива L2 — не проверял лицензию на redistribution (у Npcap OEM-модель).
- Поведение `wintun.CreateAdapter` под не-админским пользователем — не воспроизводил (нет Windows-стенда
  с Go в этой сессии); статус: вывод, не эксперимент.

## 10. Рекомендованный стек MVP (2–10 узлов)

- **Адаптер:** Wintun 0.14.1 (официальный подписанный DLL, `//go:embed`, MSI/MSM в установщике).
- **Транспорт:** свой оверлей поверх **QUIC-дейтаграмм** к своему серверу-хабу (не WireGuard), потому что нужен
  fan-out бродкаста и своя адресация «по имени сети». WireGuard — только если продукт согласен быть L3-VPN.
- **Control-plane:** QUIC + JSON/protobuf, аутентификация по имени сети и паролю (Argon2id), выдача виртуального
  IP и ключей; сервер же — TURN-подобный релей для пар, которые не пробились.
- **P2P (этап 2):** `pion/ice/v4` + `pion/turn/v5` + `pion/stun/v3`, signaling через тот же control-канал.
- **Windows:** служба `x/sys/windows/svc` (Wintun + маршруты/DNS через `winipcfg`), GUI — Wails v3 в сессии
  пользователя, IPC по named pipe, автостарт через службу.
- **Сборка:** Go 1.26+, `-H windowsgui`, подпись Authenticode, установщик WiX v6 (MSI/MSM) либо Inno 7 с
  вложенным MSI (и коммерческой лицензией).

### Следующий шаг, который стоит сделать до кода

Прототип на 200 строк: Wintun + чтение/запись пакетов + отправка их по QUIC-дейтаграмме на хаб и обратно,
с жёсткой проверкой одного сценария — «пинг между двумя машинами» и «UDP-broadcast от одной машины виден
на второй». Если второй сценарий проходит на Wintun — L2-драйвер не нужен, и проект резко дешевеет.
Если нет — сразу проектируйте под tap-windows6 и подпись драйвера, не тратя месяц на обходные пути.
