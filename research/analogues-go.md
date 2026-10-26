# Go-проекты mesh/VPN как образец архитектуры для аналога Radmin VPN

Дата исследования: **26.10.2026**. Контекст: собственный продукт на Go 1.27.1, Windows-клиент на Wails + Wintun,
свой сервер координации, вход в сеть по имени и паролю, работа за NAT без проброса портов, MVP на 2–10 узлов.

## 0. Методика и границы проверки

Что считалось доказательством (по убыванию силы):

1. **Код из клонированных репозиториев** — команды вида `git show HEAD:<path>` / `git grep`. Все ссылки `файл:строка`
   относятся к состоянию ветки по умолчанию на 26.10.2026.
2. **Файлы лицензий** в этих же клонах (`LICENSE`, `LICENSE.md`, `COPYING`).
3. **Страницы GitHub** (`github.com/<owner>/<repo>`, `/releases/latest`) — для числа звёзд и факта релиза.
4. **GitHub API** — с оговоркой: часть ответов пришла из кэша и отстаёт (см. §7).

Даты релизов получены локально из тегов:

```
git fetch --depth=1 origin '+refs/tags/*:refs/tags/*'
git for-each-ref --sort=-creatordate --count=4 --format='%(refname:short) | %(creatordate:short)' refs/tags
```

Это **дата коммита тега**, а не дата публикации релиза на GitHub; расхождение обычно 0–2 дня.
Там, где числа не удалось подтвердить, стоит «не проверено» — догадки в таблицу не подставлялись.

## 1. Сводная таблица

| Проект | Язык | Лицензия | ★ (источник, дата) | Тег/релиз (дата) | Сервер координации | Релей | NAT traversal | Адаптер Windows | GUI | Переиспользование в Go |
|---|---|---|---|---|---|---|---|---|---|---|
| **ZeroTier One** | C++ | MPL-2.0 (кроме `nonfree/` — source-available) | 17.1k (стр. GitHub, 26.10.2026) | 1.16.2 (28.05.2026) | контроллер встроен в сервис (`node/NetworkController.hpp:28`), отдельного бинаря нет | «free (but slow) relaying» через roots (README) | UDP hole punching + TCP/relay fallback | свой Ethernet TAP-драйвер (`osdep/WindowsEthernetTap.cpp`, драйвер `zttap*`) | отдельный десктоп-UI (в этом репозитории нет — **не проверено**) | нет (C++) |
| **Tailscale** | Go | BSD-3-Clause | 37.4k (стр. GitHub, 26.10.2026) | v1.104.2 (20.10.2026) | проприетарный control-сервер (в OSS-репо нет) | DERP (`derp/`, `derp/derpserver`) | disco + STUN, hole punching, DERP-фолбэк | Wintun (`tsconst/wintun/wintun.go`) | Windows/macOS-GUI **не** open source (README) | да, `tailscale.com/tsnet` + пакеты `control/controlclient`, `wgengine`, `derp` |
| **Headscale** | Go | BSD-3-Clause | 38 816 (API-снимок 24.05.2026) | v0.29.6 (24.10.2026) | сам и есть control-сервер (реализация протокола Tailscale) | **встроенный DERP** (`derp:` в `config-example.yaml`, `automatically_add_embedded_derp_region: true`) | наследует клиентский NAT-traversal Tailscale; сам только раздаёт карту DERP/эндпоинты | клиент — это tailscaled → Wintun | свой web-UI нет; управление CLI + REST/gRPC API | сервер целиком; клиентских библиотек нет |
| **NetBird** | Go | клиент/общие пакеты — **BSD-3-Clause**; `management/`, `signal/`, `relay/`, `combined/` — **AGPL-3.0** | 30 240 (API, 26.10.2026 — свежий ответ) | v0.81.5 (26.10.2026); pre-release v0.82.0-rc.1 (26.10.2026) | management-сервис (gRPC) | отдельный relay-сервис (свой протокол поверх WebSocket) + signal-сервис | ICE (pion/ice) + STUN (pion/stun, свой `stun/`) + relay-фолбэк | Wintun (`golang.zx2c4.com/wintun` + форк `netbirdio/wireguard-go`) | **Wails** (`client/ui`, wails-манифесты + `frontend/WAILS-API.md`) | клиентские пакеты импортируемы (BSD-3), но тянут форк `wireguard-go`, pion и gRPC |
| **Netmaker** | Go | Apache-2.0, кроме `pro/` (проприетарная) | 9 881 (API-снимок 01.03.2025 — устарел, на 26.10.2026 **не проверено**) | v1.7.0 (31.08.2026) | сервер: REST + MQTT/EMQX (`mq/emqx*.go`, `config/config.go:44`) | релей как роль узла (**детали не проверены**) | STUN в клиенте (`netclient/stun/stun.go`) + WireGuard endpoints | Wintun (`netclient` тащит `wintun.dll`, `config/config_windows.go`) | web-UI сервера (репозиторий UI не проверялся) | клиент вынесен в отдельный репо `gravitl/netclient` (Apache-2.0) |
| **Nebula** | Go | MIT | 17.5k (стр. GitHub) / 18 469 (API-снимок) — **расхождение, см. §7** | v1.11.2 (22.09.2026) | **нет** центрального сервера; CA `nebula-cert` + lighthouses как обычные хосты | релей = узел-релей, список приходит от lighthouse (`control.go:353`) | `punchy.go` (подтверждение hole punch) + lighthouses | Wintun (`overlay/tun_windows.go`) + WFP-фильтр (`wfp/`) | официального GUI нет (CLI) | MIT, но корень — `package main`; импортируются подпакеты (`overlay`, `cert`, `udp`, `firewall`, `wintun`) |
| **EasyTier** | **Rust** | **LGPL-3.0** | 13 341 (API-снимок 28.08.2026) | v2.7.0 (05.10.2026); nightly (25.10.2026) | нет (децентрализовано); опционально `easytier-web` | через любой узел (можно запретить: `disable_relay_data`, release notes v2.6.3) | UDP/IPv6 hole punch (`instance/udp_hole_punch.rs`, `common/stun.rs`), UPnP | Wintun (`easytier/src/arch/windows.rs`, `third_party/arm64/wintun.dll`) | Tauri (`easytier-gui`, `src-tauri`) + web-консоль | только через `easytier-go` = **wazero/WASM**, не нативный Go |
| **innernet** | Rust | MIT | не проверено (API-снимок 2021 г.: 2 251) | v2.0.0 (02.07.2026) | `innernet-server` (SQLite, выдача конфигов) | **нет** | только «endpoint, который видит сервер» + ручной override; нужен проброс порта на сервер | официально не поддерживается (README: «only been officially tested on Linux and macOS») | нет (CLI) | Rust-крейты (`innernet-client-core`) |

## 2. Что важно по каждому проекту (коротко)

### ZeroTier One
- Лицензия: `LICENSE.txt` — MPL-2.0 для `node/`, `osdep/`, `service/`; `nonfree/` — «source available» с отдельной лицензией.
  Для копирования архитектуры это не мешает, для заимствования кода — требует аккуратности.
- Координация: контроллер — часть того же сервиса (`node/NetworkController.hpp:28`), т.е. «сервер» и «узел» — один бинарь.
  Для продукта вроде Radmin это удобно: меньше сущностей для установки.
- Сеть: L2-эмуляция (Ethernet) поверх P2P; на Windows — собственный подписанный TAP-драйвер, а не Wintun.
  Это самый дорогой путь для MVP: свой драйвер + L2.
- Вывод: архитектурно интересен как «L2 + один бинарь», но стек (C++, свой NDIS-драйвер) для Go-продукта не переносим.

### Tailscale
- Единственный из списка, где **control-протокол открыт и документирован кодом**: `control/ts2021/` (клиент),
  `control/controlbase`, `control/controlhttp` (транспорт), `tailcfg` (типы сообщений).
- Windows-GUI проприетарны, но демон `tailscaled` — BSD-3. `tsnet` позволяет встроить узел Tailscale в Go-программу
  (готовый «клиент в процессе»), что для MVP — самый быстрый способ получить NAT traversal, но вы наследуете
  зависимость от чужого control-сервера и его модели идентичности.
- Вывод: брать не код, а **форму протокола** (её же реализует Headscale).

### Headscale
- Единственный полностью открытый control-сервер в списке; BSD-3-Clause; ~38,8k звёзд.
- Публикует не GitHub Releases, а теги/контейнеры: `releases/latest` до сих пор указывает на v0.23.0 (18.09.2024),
  при этом последний тег — v0.29.6 (24.10.2026). Это важно, если автоматизировать проверку версий.
- Подробно разобран в §3.

### NetBird
- Единственный, кто **разделил три сервиса** (management / signal / relay) и при этом сделал десктопный UI на Wails —
  то есть ближе всех к стеку заказчика. Подробно в §4.
- Лицензионная асимметрия: клиент BSD-3, серверные каталоги AGPL-3.0. Для продукта с закрытым сервером это
  означает: клиентские пакеты можно брать, серверные — нельзя без раскрытия.

### Netmaker
- Ставка на «WireGuard + автоматизация»: сервер раздаёт конфиги через MQTT (EMQX), gRPC используется только для
  аудита/flow/SIEM (`grpc/auditlogs`, `grpc/flow`, `grpc/siem`), т.е. это не control-plane на gRPC.
- Клиент вынесен в отдельный репозиторий `gravitl/netclient` (Apache-2.0): там `stun/`, `wireguard/`, `wintun.dll`.
- Для MVP на 2–10 узлов MQTT-брокер — лишняя инфраструктура: брокер, REST, БД, gRPC-сайдкары.

### Nebula
- Самый «минималистичный control plane»: сервера нет вообще, есть CA и lighthouses (обычные узлы с флагом).
  Релеи — тоже обычные узлы; список релеев приходит в hostmap от lighthouse.
- MIT + чистый Go + Wintun + собственный WFP-фильтр. Хороший источник идей для «mesh без сервера», но для Radmin-аналога
  с входом по имени/паролю нужен слой аккаунтов и приглашений, которого у Nebula нет.

### EasyTier
- Не Go, а Rust (workspace, edition 2024, rust-version 1.95). LGPL-3.0.
- Модель входа — ровно то, что нужно продукту: `--network-name` + `--network-secret` (+ список пиров), без аккаунтов.
  В коде: `easytier-core/src/config/api.rs:33` (`network_name`), `:34` (`network_secret`).
- Объём кода — см. §5: он **не компактный**.

### innernet
- Модель «инвайт-файл → одноразовое погашение → выдача конфига» ближе всего к «входу по паролю», но:
  нет релея, нужен проброс порта на сервер (README:46), Windows официально не поддерживается.
- Для требования «работа за NAT без проброса портов» не подходит как образец data plane; интересен только
  церемонией выдачи доступа (invitation, `override-endpoint`).

## 3. Фокус 1 — Headscale: протокол клиент↔control, ключи, формат конфигурации

### 3.1 Транспорт и HTTP-API

Headscale не изобретает протокол: он реализует **TS2021 (Noise)** Tailscale, переиспользуя библиотеки Tailscale.

- `hscontrol/noise.go:60` — `ts2021UpgradePath = "/ts2021"`.
- Апгрейд делает `controlhttp/controlhttpserver` + `controlbase.Conn` (импорты в шапке `hscontrol/noise.go`),
  т.е. рукопожатие — **Noise IK** на Curve25519 с ключом машины; после апгрейда соединение «становится» HTTP/2.
- Внутри туннеля поднимается обычный `chi`-роутер:
  - `hscontrol/noise.go:166` — `r.Post("/register", ns.RegistrationHandler)`
  - `hscontrol/noise.go:167` — `r.Post("/map", ns.PollNetMapHandler)`
  - далее `/ssh/action/{src}/to/{dst}`, `/set-dns`, `/whoami`, `/id-token`, `/c2n`, `/update-health` — часть
    заглушки (`NotImplementedHandler`).
- Тела сообщений — **JSON из `tailcfg`**: `tailcfg.RegisterRequest` / `RegisterResponse`, `MapRequest` / `MapResponse`.
  Никакого protobuf на этом канале нет.
- `/map` — долгоживущий поток: клиент присылает `MapRequest`, сервер держит соединение и льёт `MapResponse`
  (в коде есть ветка для `mapRequest.Stream`, ответ «expired» для удалённого узла, `writeMapResponse(writer, mapRequest.Compress, ...)` —
  т.е. предусмотрено сжатие карты).
- Есть «early payload»: первые 9 байт от сервера — либо HTTP/2 SETTINGS, либо заголовок `\xff\xff\xffTS` + длина +
  JSON `tailcfg.EarlyNoise` (комментарий в `hscontrol/noise.go`, const `earlyPayloadMagic`). Это оптимизация, чтобы
  клиент сразу получил версию протокола/параметры.

Отдельно от клиентского протокола у Headscale есть **свой административный API** (`hscontrol/api/v1/*`, `apiv2_*`):
ключи с префиксом `hskey-api-` (`hscontrol/db/api_key.go:14`), REST + gRPC (`proto/headscale/v1`).

### 3.2 Как выдаются ключи и как регистрируется узел

Три независимые пары ключей (модель узла, `hscontrol/types/node.go`): `MachineKey`, `NodeKey`, `DiscoKey`,
плюс `Endpoints`, `Hostinfo`, IP-адреса, владелец, теги, срок жизни. Смысл: ключ машины живёт долго и
идентифицирует устройство; node-ключ ротируется; disco-ключ нужен для NAT traversal.

Регистрация по pre-auth key:

1. Клиент шлёт `POST /register` с `RegisterRequest`, где `Auth.AuthKey` — pre-auth key.
2. Сервер: `handleRegister` (`hscontrol/auth.go:38`) → `handleRegisterWithAuthKey` (`hscontrol/auth.go:412`) →
   `state.HandleNodeFromPreAuthKey`.
3. Ошибки маппятся в HTTP: `gorm.ErrRecordNotFound` → **401 «invalid pre auth key»** (`hscontrol/auth.go:419-424`),
   прочие `types.PAKError` → тоже 401.
4. Успех — `RegisterResponse{MachineAuthorized: true, NodeKeyExpired: ..., User: ..., Login: ...}` (`hscontrol/auth.go:463`).
5. Дальше узел живёт в `/map`, а истёкший node-ключ перевыпускается по флагу `NodeKeyExpired`.

Формат самого ключа (`hscontrol/db/preauth_keys.go`):

- префикс `hskey-auth-` (`:60`), затем `generateSecret(prefix)` → `keyStr, identifier, hash`;
- в БД лежит `Credential{Kind: CredentialPreAuthKey, Identifier, Hash, UserID, Reusable, Ephemeral, CreatedAt, Expiration, Tags}`
  — **секрет хранится хешем**, в открытом виде показывается один раз;
- флаги: `reusable`, `ephemeral`, срок жизни, ACL-теги (валидация «тег должен начинаться с `tag:`», `validateACLTags`);
- ограничение: ключ должен быть либо привязан к пользователю, либо тегирован (`ErrPreAuthKeyNotTaggedOrOwned`).

Интерактивный путь (`handleRegisterInteractive`, `hscontrol/auth.go:479`) — OIDC/веб-подтверждение: сервер отдаёт
`AuthURL`, клиент открывает браузер, админ подтверждает. Это аналог «одобрить устройство в панели».

### 3.3 Формат конфигурации

Две разные вещи, которые легко перепутать:

- **Конфиг сервера** — YAML `config.yaml` (`config-example.yaml`): `server_url`, `listen_addr`, `metrics_listen_addr`,
  `derp:` (встроенный DERP: `region_id: 999`, `region_code: "headscale"`, `automatically_add_embedded_derp_region: true`,
  `urls`, `paths`, `auto_update_enabled`), TLS, БД, политики.
- **Конфиг узла** — не headscale-формат: узел остаётся клиентом Tailscale и хранит свои `Prefs`/state у себя,
  а с сервера получает `MapResponse` (карта сети: свои адреса, пиры, DNS, DERP-регионы). Подключение к своему
  серверу — флаг клиента `--login-server`.

### 3.4 Можно ли повторить дизайн

Да, и это самый переносимый рецепт из списка. Что копировать (архитектурно, не код):

1. **Транспорт с взаимной аутентификацией до всякого HTTP**: Noise IK (или TLS + mTLS) → внутри HTTP/2 → JSON-сообщения.
   Даёт: аутентификацию по ключу машины, один порт, обычные HTTP-роутеры и middleware.
2. **Два эндпоинта**: `POST /register` (короткий обмен) и `POST /map` (долгий поток состояния). Клиенту не нужен polling —
   сервер сам пушит изменения; для 2–10 узлов это буквально «одно соединение на узел».
3. **Три ключа** (machine / node / disco) с разными жизненными циклами и ротацией node-ключа по флагу в ответе.
4. **Ключи доступа как credential-запись**: префикс + identifier + hash в БД, флаги reusable/ephemeral/expiry/tags,
   один показ секрета. Это дешево и сразу даёт «вход по паролю/инвайту» с отзывом.
5. **Встроенный релей как опция конфигурации** (`derp:`-секция), а не отдельный продукт — для MVP критично.

Чего не копировать: привязку к `tailcfg` (десятки полей, половина не нужна), DERP-карты, capability-versioning —
это цена совместимости с чужим клиентом, которая вашему продукту не нужна.

## 4. Фокус 2 — NetBird: разделение management / signal / relay и протоколы

### 4.1 Три сервиса и их протоколы

Все три поднимаются из одного репозитория и лицензионно разделены по каталогам; для «всё в одном» есть `combined/`
(сборка в один контейнер/бинарь — в каталоге лежат только Dockerfile-варианты, сам код сборки не проверялся).

**Management** — gRPC (`shared/management/proto/management.proto`):

```
service ManagementService {
  rpc Login(EncryptedMessage) returns (EncryptedMessage) {}     // :14
  rpc Sync(EncryptedMessage) returns (stream EncryptedMessage) {} // :20
  rpc GetServerKey(Empty) returns (ServerKeyResponse) {}
  rpc SyncMeta(EncryptedMessage) returns (Empty) {}
  rpc Job(stream EncryptedMessage) returns (stream EncryptedMessage) {}
  ...
}
```

`Sync` — серверный стрим: это полный аналог headscale-овского `/map`, только на gRPC. `Job` — bidi-стрим для
команд на узел.

**Signal** — тоже gRPC, не WebSocket (`shared/signal/proto/signalexchange.proto`):

```
service SignalExchange {
  rpc Send(EncryptedMessage) returns (EncryptedMessage) {}                       // :11
  rpc ConnectStream(stream EncryptedMessage) returns (stream EncryptedMessage) {} // :13
}
```

И тело — не «сырой» ICE-обмен: `EncryptedMessage{key, remoteKey, body}`, где `body` зашифровано ключами WireGuard
(комментарий в proto: «encrypted with the Wireguard private key and the remote Peer key»). Внутри `Body` —
`OFFER / ANSWER / CANDIDATE / MODE / GO_IDLE / HEARTBEAT`, плюс `wgListenPort`, `relayServerAddress`, `rosenpassConfig`.
То есть signal — это **rendezvous-сервер, который не видит содержимое** (не может подменить кандидатов, не зная ключей).

**Relay** — свой протокол поверх **WebSocket** (`relay/doc.go`): «the transport layer is the WebSocket protocol…
a custom protocol and message format… optionally the server can use TLS». Адрес релея приходит пиру через signal
(`relayServerAddress`), что позволяет держать несколько релей-инстансов.

Итого ответ на вопрос «gRPC или WebSocket»: **management и signal — gRPC (HTTP/2)**, **relay — WebSocket**.
(Ранние версии NetBird использовали для signal WebSocket; в текущем коде это gRPC-стрим.)

### 4.2 NAT traversal

- ICE: `client/iface/bind/ice_bind.go`, `relay_bind.go` — endpoints собираются ICE-агентом (pion).
- STUN: зависимость `github.com/pion/stun/v2` и `/v3` в `go.mod`, свой STUN-сервер в `stun/`.
- Фолбэк: если ICE не поднял P2P — трафик идёт через relay-сервис (WebSocket-канал, свой мультиплексор).

### 4.3 Windows-клиент и GUI

- Адаптер: Wintun — `golang.zx2c4.com/wintun` в `go.mod` (indirect) + форк `github.com/netbirdio/wireguard-go`;
  установщик кладёт драйвер (`client/installer.nsis`, `client/netbird.wxs`).
- GUI: **Wails** — `client/ui/build/windows/nsis/wails_tools.nsh`, `client/ui/build/wails.exe.manifest`,
  `client/ui/frontend/WAILS-API.md`. То есть NetBird уже решил ровно ту связку «Go-демон + Wails-оболочка»,
  которая планируется у заказчика.

### 4.4 Лицензия клиента

`LICENSE` (корень): «This BSD-3-Clause license applies to all parts of the repository **except** for the directories
`management/`, `signal/`, `relay/` and `combined/`. Those directories are licensed under AGPLv3.»

Практический вывод: клиентские пакеты (`client/`, `iface/`, `route/`, `dns/`, `shared/`) — BSD-3, их можно
переиспользовать в закрытом продукте. Серверные каталоги — AGPL: либо не трогать, либо открывать свой сервер.

### 4.5 Что копировать

1. **Три роли, один бинарь**: management (состояние + политика), signal (rendezvous, ничего не знает о трафике),
   relay (пересылка). Даже если в MVP это три слушателя в одном процессе — границы пакетов должны быть такими же.
2. **Шифрование полезной нагрузки rendezvous-сообщений ключами data plane** — снимает доверие к signal-серверу.
3. **gRPC-стрим как канал состояния** (`Sync`) и **bidi-стрим для команд** (`Job`) — дешевле, чем свой протокол.
4. **Relay поверх WebSocket с TLS** — простое горизонтальное масштабирование и совместимость с любыми прокси.
5. Разделение лицензий по каталогам — организационный приём, который стоит воспроизвести заранее.

## 5. Фокус 3 — EasyTier: насколько компактен и годится ли как референс MVP

### 5.1 Размер кода (измерено локально)

| Крейт | Файлов `.rs` | Строк |
|---|---|---|
| `easytier-core` (ядро: peers, tunnel, connectivity, rpc, gateway) | 279 | **115 220** (без тестов — 107 411) |
| `easytier` (инстанс, arch, vpn_portal, web_client, igd/upnp) | 140 | **56 327** |
| `easytier-proto` | — | 3 096 |
| Весь workspace (включая `easytier-web`, GUI, contrib, тесты) | — | **220 852** |

Замер: `Get-ChildItem -Recurse -Filter *.rs | Get-Content | Measure-Object -Line`.

Вывод по компактности: **EasyTier не компактен**. Ядро — больше сотни тысяч строк Rust; это продукт с QUIC,
WebSocket/WSS-туннелями, OSPF-подобной маршрутизацией, ACL, subnet proxy, VPN-portal (WireGuard-сервер),
DHCP, UPnP/IGD, WASM-хостом. Как «лёгкий референс на 10 узлов» он не годится; как каталог решений — да.

### 5.2 Что в нём действительно стоит изучить

- **Модель входа**: `--network-name` + `--network-secret` + список пиров (`-p tcp://ip:11010`). В коде конфигурация
  сети: `easytier-core/src/config/api.rs:33` (`result.network_name = ...`), `:34` (`network_secret`).
  Это буквально «имя сети + пароль», как в Radmin, без аккаунтов и без сервера.
- **Децентрализованность**: «no distinction between server and client»; публичные shared-узлы выполняют роль
  rendezvous + релея, при отказе P2P трафик идёт через них (README, раздел Quick Networking).
- **Hole punching**: `easytier/src/instance/udp_hole_punch.rs` (платформенный адаптер порт-маппинга),
  `easytier/src/common/stun.rs`; поддержка IPv4/IPv6 и UPnP (`--disable-upnp` в v2.6.2+).
- **Релей как роль узла, а не сервис**: есть рантайм-флаг `disable_relay_data` (release notes v2.6.3) — узел
  остаётся в control plane, но не тратит трафик на пересылку. Дешёвый и полезный приём для MVP.
- **Windows**: Wintun (`easytier/src/arch/windows.rs`, `easytier/third_party/arm64/wintun.dll`),
  туннель через форк `tun-easytier` (rust-tun) + `smoltcp`.

### 5.3 Почему не годится как Go-библиотека

- Язык — Rust; LGPL-3.0 (для закрытого продукта — вопросы к статической линковке и модификациям).
- `easytier-go` — не нативный Go-порт: `easytier-go/go.mod` зависит от `github.com/metacubex/wazero`,
  т.е. Rust-ядро исполняется как **WASM в Go-процессе** (`host.go` — обёртка над `internal/host`, состояния
  `StateCreated…StateStopped`). Это рабочий, но экзотический путь: +WASM-рантайм, ограничения по сокетам/драйверам.

**Итог по фокусу 3**: EasyTier — отличный источник UX-решений (вход по имени/паролю, shared-узлы, флаги relay),
плохой источник «компактного кода» и не источник Go-библиотек.

## 6. Вывод: 2–3 образца и что именно копировать

### Образец №1 — Headscale (control plane и модель доступа). Главный.

Копировать:
1. Один транспорт с mTLS/Noise, внутри — HTTP/2 + JSON; два эндпоинта: `POST /register` и `POST /map` (долгий поток).
2. Три ключа узла (machine / node / disco) и ротацию node-ключа флагом в ответе (`NodeKeyExpired`).
3. Ключи доступа: префикс + identifier + **hash в БД**, флаги `reusable` / `ephemeral` / `expiry` / `tags`,
   одноразовый показ секрета, 401 на неизвестный/просроченный ключ.
4. Встроенный релей как секция конфига (аналог `derp:`), а не отдельный продукт.
5. Административный API отдельно от клиентского протокола (ключи `hskey-api-`, REST/gRPC) — чтобы UI на Wails
   не лез в клиентский канал.

Не копировать: `tailcfg`-совместимость, DERP-регионы, capability-versioning.

### Образец №2 — NetBird (разделение сервисов и Windows-стек). Обязательный.

Копировать:
1. Разделение management / signal / relay на уровне пакетов, даже если в MVP это один процесс с тремя слушателями.
2. Signal как «слепой» rendezvous: тело сообщений шифруется ключами data plane (`EncryptedMessage{key, remoteKey, body}`).
3. `Sync`-стрим как канал состояния и `Job`-стрим как канал команд (gRPC), relay — WebSocket + TLS.
4. Связку Wintun + форк/обёртка wireguard-go + NSIS-установщик, и Wails-оболочку поверх демона (`client/ui`).
5. Разделение лицензий по каталогам (клиент — разрешительная, сервер — AGPL) как приём, если сервер будет закрытым.

### Образец №3 — Nebula (минимализм data plane и relay-модель). Опциональный, но полезный.

Копировать:
1. «Релей — это роль узла, а не сервис»: список релеев приходит от координатора (`control.go:353` итерация по
   `hostMap.Relays`), выбор — на клиенте.
2. `punchy`-подход: hole punch подтверждается данными, а не только обменом кандидатов (`punchy.go`).
3. MIT-лицензия и чистый Go — единственный в списке, чьи подпакеты можно спокойно смотреть и переиспользовать.

### Что не брать за образец
- **EasyTier** — как «лёгкий MVP»: 115k строк ядра на Rust; брать только UX-модель (имя сети + секрет, shared-узлы).
- **innernet** — нет релея и нужен проброс порта (README:46), Windows не поддерживается; несовместимо с требованием
  «за NAT без проброса портов».
- **Netmaker** — MQTT-брокер в контуре ради 10 узлов; клиент в отдельном репозитории, связка сложнее.
- **ZeroTier** — L2 + собственный NDIS-драйвер: непереносимо на Go/Wintun в рамках MVP.

### Рекомендуемая форма MVP (сводка решений)

| Слой | Решение | Откуда |
|---|---|---|
| Вход в сеть | имя сети + секрет (HKDF → PSK) **или** одноразовый инвайт-ключ с hash в БД | EasyTier (UX) + Headscale (ключи) |
| Control-канал | TLS/mTLS + HTTP/2, `POST /register` + `POST /map` (поток), JSON | Headscale |
| Идентичность узла | machine / node / disco ключи, node ротируется | Headscale |
| Data plane | WireGuard (wireguard-go) + Wintun | NetBird, Tailscale |
| NAT traversal | STUN (pion/stun) → hole punch → relay-фолбэк | NetBird, Nebula |
| Relay | отдельный сервис/роль узла; в MVP — встроенный в сервер | Headscale (`derp:`) + Nebula |
| Сервер | один бинарь: management + signal + relay-слушатель | NetBird (роли), Headscale (простота) |
| UI | Wails-оболочка над демоном, управление через локальный API | NetBird (`client/ui`) |

## 7. Расхождения источников и что осталось непроверенным

1. **Звёзды: веб против API.** Для nebula страница GitHub показывает **17.5k**, а `api.github.com/repos/slackhq/nebula`
   вернул **18 469** при `updated_at` = 26.10.2026. Для netbird `/repos/netbirdio/netbird` отдал `pushed_at`
   25.07.2026, тогда как `/repositories/357897199` (тот же репозиторий по id) — 26.10.2026 и 30 240 звёзд.
   Вывод: ответы API приходят из кэша с разным временем снимка. Поэтому в таблице у каждой цифры указан источник
   и дата снимка; сравнивать звёзды между проектами следует как порядок величины, а не как точное число.
2. **Устаревшие снимки.** netmaker — 9 881 ★ (снимок 01.03.2025), innernet — 2 251 ★ (снимок 30.07.2021).
   Для innernet актуальное число получить не удалось → «не проверено».
3. **Headscale и GitHub Releases.** `releases/latest` = v0.23.0 (18.09.2024), при этом тег v0.29.6 от 24.10.2026.
   Причина — проект не публикует GitHub Releases; ориентироваться на теги/контейнеры.
4. **Nebula и «latest».** `releases/latest` = v1.10.3 (06.02.2026), последний тег — v1.11.2 (22.09.2026).
5. **NetBird и «latest».** `releases/latest` = v0.73.2 (22.06.2026), последний стабильный тег — v0.81.5 (26.10.2026),
   плюс pre-release v0.82.0-rc.1 того же дня. Похоже, указатель «latest» выставляется вручную.
6. **Не проверено:** наличие/состояние GUI у ZeroTier и Netmaker (UI в отдельных репозиториях), детали релей-модели
   Netmaker («relay» как роль узла), точные порты по умолчанию у NetBird, актуальные звёзды innernet.
7. **Даты релизов** — это даты коммитов тегов, а не публикаций на GitHub (`published_at`); расхождение не измерялось.

## 8. Приложение: как воспроизвести проверку

```powershell
# 1. Клоны (shallow)
git clone --depth 1 https://github.com/juanfont/headscale.git
git clone --depth 1 https://github.com/netbirdio/netbird.git
git clone --depth 1 https://github.com/EasyTier/EasyTier.git
git clone --depth 1 https://github.com/gravitl/netmaker.git
git clone --depth 1 https://github.com/gravitl/netclient.git
git clone --depth 1 https://github.com/slackhq/nebula.git
git clone --depth 1 https://github.com/tonarino/innernet.git
git clone --depth 1 https://github.com/zerotier/ZeroTierOne.git
git clone --depth 1 https://github.com/tailscale/tailscale.git

# 2. Даты релизов
git -C <repo> fetch --depth=1 origin '+refs/tags/*:refs/tags/*'
git -C <repo> for-each-ref --sort=-creatordate --count=4 --format='%(refname:short) | %(creatordate:short)' refs/tags

# 3. Протокол Headscale
git -C headscale show HEAD:hscontrol/noise.go | Select-String "ts2021UpgradePath|r.Post|earlyPayloadMagic"
git -C headscale show HEAD:hscontrol/auth.go   | Select-Object -Skip 405 -First 70

# 4. Протоколы NetBird
git -C netbird show HEAD:shared/management/proto/management.proto | Select-String "rpc "
git -C netbird show HEAD:shared/signal/proto/signalexchange.proto | Select-Object -First 70
git -C netbird show HEAD:relay/doc.go | Select-Object -First 20

# 5. Объём EasyTier
Get-ChildItem EasyTier\easytier-core\src -Recurse -Filter *.rs | Get-Content | Measure-Object -Line
```

Ключевые точечные ссылки, использованные в выводах:

- `headscale/hscontrol/noise.go:60` — путь `/ts2021`; `:166` — `POST /register`; `:167` — `POST /map`.
- `headscale/hscontrol/auth.go:412` — регистрация по pre-auth key; `:419-424` — 401 на неверный ключ; `:463` — `MachineAuthorized: true`.
- `headscale/hscontrol/db/preauth_keys.go:60` — префикс `hskey-auth-`; `types.Credential` — hash/reusable/ephemeral/tags.
- `headscale/hscontrol/db/api_key.go:14` — префикс `hskey-api-`.
- `netbird/shared/management/proto/management.proto:14,20` — `Login`, `Sync` (server-stream).
- `netbird/shared/signal/proto/signalexchange.proto:11,13` — `Send`, `ConnectStream` (bidi).
- `netbird/relay/doc.go:2-8` — «the transport layer is the WebSocket protocol».
- `netmaker/config/config.go:44` — `EmqxRestEndpoint` (конфиг через MQTT/EMQX).
- `nebula/control.go:353` — перебор `c.f.hostMap.Relays`.
- `zerotier/ZeroTierOne/node/NetworkController.hpp:28` — `class NetworkController`.
- `EasyTier/easytier-core/src/config/api.rs:33,34` — `network_name`, `network_secret`.
- `innernet/README.md:20` — нужен coordination server; `:46` — нужен port forwarding.
