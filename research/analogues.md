# Аналоги Radmin VPN / LogMeIn Hamachi: разбор open-source проектов

**Дата среза данных:** 26.10.2026.
**Источники цифр:** GitHub REST API — `GET /repos/{owner}/{repo}` (звёзды, лицензия, язык), `GET /repos/{owner}/{repo}/commits?per_page=1` (последний коммит default-ветки), `GET /repos/{owner}/{repo}/releases/latest` (последний релиз). URL каждого репозитория указан в таблице 1. Там, где API отдавал `NOASSERTION` или лицензия смешанная, текст лицензии прочитан из файла `LICENSE*` в репозитории (ссылки в примечаниях).
**Задача, под которую собран материал:** свой аналог Radmin VPN на Go, Windows-клиент, свой сервер координации, MVP на 2–10 узлов, вход в сеть по имени и паролю, работа за NAT без проброса портов.

---

## 1. Что искали и что нашли

Выполнены четыре обязательных запроса плюс уточняющие:

| Запрос | Что дал |
|---|---|
| `radmin vpn alternative open source self-hosted` | выдачу из обзоров и Reddit; практической ценности мало, кроме упоминания **Lanemu** и **Twingate** |
| `hamachi clone github open source virtual LAN` | **Lanemu** (GitLab), **orbitlan**, статья Tailscale «A modern replacement for Hamachi» |
| `virtual lan over internet github project tun tap p2p` | **minilan-vpn** («Free Radmin/Hamachi-style virtual LAN»), `tun-rs` (библиотека TUN/TAP для Rust) |
| `mesh vpn golang github open source` | **webmesh** (Go), **meshvpn** (Go control plane), **skx/simple-vpn**, тред про «Knoten» |

Ключевой вывод по поиску: **отдельного зрелого open-source клона именно Radmin VPN нет**. То, что находится под формулировкой «Radmin/Hamachi-style», — это либо проекты на 1–2 звезды, созданные в 2026 году (minilan-vpn, orbitlan), либо **Lanemu** — Java-форк P2PVPN, который ближе всех по UX, но без настоящего hole punching. Все зрелые проекты решают ту же транспортную задачу, но с другой моделью доступа (аккаунт/IdP/сертификаты вместо «имя сети + пароль»).

---

## 2. Таблица 1. Метаданные (проверено через GitHub API 26.10.2026)

| Проект | Репозиторий | Язык | Лицензия (факт) | ★ | Последний коммит | Последний релиз |
|---|---|---|---|---|---|---|
| ZeroTier One | `github.com/zerotier/ZeroTierOne` | C++ | MPL-2.0 (ядро), `nonfree/` — source-available, SDK libzt — BSL-1.1 | 17 211 | 22.07.2026 | 1.16.2 (28.05.2026) |
| Tailscale | `github.com/tailscale/tailscale` | Go | BSD-3-Clause (GUI для Windows/macOS — закрытые) | 38 122 | 26.10.2026 | v1.104.2 (21.10.2026) |
| Headscale | `github.com/juanfont/headscale` | Go | BSD-3-Clause | 45 068 | 24.10.2026 | v0.29.6 (24.10.2026) |
| NetBird | `github.com/netbirdio/netbird` | Go | BSD-3-Clause, **кроме** `management/`, `signal/`, `relay/`, `combined/` — AGPL-3.0 | 30 240 | 26.10.2026 | v0.81.4 (24.10.2026) |
| Netmaker | `github.com/gravitl/netmaker` | Go | Apache-2.0, **кроме** `pro/` (проприетарная) | 11 838 | 25.10.2026 | v1.7.0 (31.08.2026) |
| Nebula | `github.com/slackhq/nebula` | Go | MIT | 18 469 | 26.10.2026 | v1.11.2 (22.09.2026) |
| n2n | `github.com/ntop/n2n` | C | GPL-3.0 | 7 046 | 17.10.2026 | 3.0 (27.10.2021) |
| tinc | `github.com/gsliepen/tinc` | C | GPL-2.0-or-later | 2 259 | 27.06.2026 | релизов на GitHub нет (теги) |
| EasyTier | `github.com/EasyTier/EasyTier` | Rust | LGPL-3.0 | 14 349 | 26.10.2026 | v2.6.4 (12.05.2026) |
| SoftEther VPN | `github.com/SoftEtherVPN/SoftEtherVPN` | C | Apache-2.0 | 13 673 | 20.10.2026 | 5.2.5188 (18.07.2025) |
| innernet | `github.com/tonarino/innernet` | Rust | MIT | 5 568 | 28.07.2026 | v2.0.0 (02.07.2026) |
| wg-access-server | `github.com/Place1/wg-access-server` | TypeScript + Go | MIT | 1 850 | **17.01.2021** (ветка `master`) | релизов нет |

Проверка лицензий по файлам (не по метке GitHub): `ZeroTierOne/LICENSE.txt` → «See LICENSE-MPL.txt for all code in node/, osdep/, service/… See nonfree/LICENSE.md for all non-free ("source available") portions»; `netbird/LICENSE` → «This BSD-3-Clause license applies to all parts of the repository except for the directories management/, signal/, relay/ and combined/. Those directories are licensed under the GNU Affero General Public License version 3.0»; `netmaker/LICENSE.md` → «Content outside of the above mentioned directories or restrictions above is available under the "Apache Version 2.0" license».

**Важная поправка к распространённому мнению:** Netmaker — не AGPL, а Apache-2.0 с проприетарным `pro/`. NetBird — наоборот, AGPL именно в серверной части (то есть сервер координации нельзя взять в закрытый продукт). ZeroTier — не BUSL целиком: ядро под MPL-2.0, но SDK `libzt` — BSL-1.1, а каталог `nonfree/` — source-available.

---

## 3. Таблица 2. Архитектура

| Проект | Сервер координации | Data plane | NAT traversal | Виртуальный интерфейс на Windows |
|---|---|---|---|---|
| ZeroTier One | Roots («planet» из 4 серверов ZeroTier + свои «moons») отдельно от network controller (CA сети) | Свой протокол: VL1 (P2P-транспорт) + VL2 (эмуляция Ethernet, аналог VXLAN) | Rendezvous через root → hole punching; port prediction для симметричного NAT; UPnP/NAT-PMP; relay через root как fallback | Свой TAP-драйвер: `ext/bin/tap-windows-ndis6/x64/zttap300.inf` — «ZeroTier One Virtual Network Port NDIS6 Driver… Based on the OpenVPN tap-windows6 driver version 9.21.1», L2, MTU 2800, GPLv2 |
| Tailscale | Coordination server (в SaaS закрыт; в репозитории — клиентская часть `control/`) | WireGuard (userspace `tailscale/wireguard-go` или ядро) | disco-пинг + STUN-подобное определение адресов через координацию; fallback — DERP-релеи | **Wintun**: `go.mod` содержит `golang.zx2c4.com/wintun` и `golang.zx2c4.com/wireguard/windows v1.0.1` |
| Headscale | Сам и есть сервер координации (реализация control-сервера Tailscale), одна tailnet | Нет своего: data plane — клиент Tailscale (WireGuard) | Наследуется от клиента Tailscale (disco + DERP) | Через клиент Tailscale → Wintun |
| NetBird | Три сервиса: Management (состояние, IP), Signal (обмен кандидатами), Relay | WireGuard (`github.com/netbirdio/wireguard-go`, форк) | ICE через `pion/ice`, кандидаты через STUN, обмен через Signal, fallback — Relay | `go.mod`: `golang.zx2c4.com/wireguard/windows v0.5.3` (Wintun-семейство); в релизе есть `netbird-ui-windows_0.81.4_windows_amd64.tar.gz` |
| Netmaker | Server (конфигурация + очередь сообщений MQ) + агенты netclient | WireGuard, по возможности ядровой | «WireGuard, STUN и TURN»; STUN — с v0.18.0; есть TCP-uplink поверх WebSocket при блокировке UDP; выделенные relay-серверы — в Pro | Не проверено по коду (в релизе только `nmctl-windows-amd64.exe`) |
| Nebula | Сервера нет: lighthouses (discovery) + опциональные relay-узлы | Свой протокол на Noise Protocol Framework, AES-256-GCM | UDP hole punching через lighthouse; режим relay-узла есть в конфиге (в README не описан) | **Wintun**: `go.mod` содержит `golang.zx2c4.com/wintun` |
| n2n | supernode (обязателен, публичный порт) | Свой L2-протокол поверх UDP | Прямой UDP P2P, при неудаче — relay через supernode | TAP (драйвер tap-windows); официального GUI нет, сторонний — HappyNet |
| tinc | Нет: равноправные демоны, `ConnectTo` | Свой протокол: meta-соединение (TCP) для состояния + UDP-туннель для данных | Адреса узнаются по meta-соединению, далее прямой UDP; при неудаче — передача через промежуточный узел | TAP/TUN-устройство; под Windows — tap-драйвер, настройка вручную |
| EasyTier | Не обязателен: децентрализованно, есть общие публичные узлы для discovery/relay | Свой протокол, транспорт UDP/TCP/WSS/QUIC; шифрование AES-GCM или WireGuard (`boringtun`) | Заявлен обход «NAT4–NAT4» (симметричный NAT), UDP и IPv6 traversal, `stun_codec` в зависимостях, relay через публичные узлы | Форк `rust-tun` (`tun-easytier`); на Windows это Wintun-путь — **прямо не проверено** |
| SoftEther VPN | Virtual Hub (L2-коммутатор) на сервере; встроенный Dynamic DNS | SSL-VPN поверх HTTPS/443 (плюс OpenVPN, IPsec, L2TP, SSTP, EtherIP) | «SSL-VPN Tunneling on HTTPS to pass through NATs and firewalls» + NAT-T | Собственный драйвер «SoftEther VPN Client Virtual Network Adapter» (NDIS), не TAP и не Wintun |
| innernet | Coordination server: раздаёт пирам endpoint-информацию и ACL по CIDR | WireGuard | Отдельного NAT traversal нет: пиры должны быть достижимы напрямую | Через WireGuard-клиент ОС (Wintun) — **не проверено** |
| wg-access-server | Сервер — сам концентратор (hub-and-spoke), web-UI + gRPC-Web | WireGuard | Нет: нужен публичный endpoint сервера | Клиент — официальный WireGuard (Wintun) |

---

## 4. Заметки по проектам

### 4.1 ZeroTier One — самая близкая к Radmin модель данных
Двухуровневая архитектура: **VL1** — криптографически адресуемый P2P-транспорт, **VL2** — эмуляция Ethernet «похожая на VXLAN» (`docs.zerotier.com/protocol/`). Ключевое для нашей задачи — механика установления связи, описанная там же дословно: узел без прямого пути шлёт пакет на root; root отвечает сообщением *rendezvous* с подсказками обоим концам; «A and B get their rendezvous messages and attempt to send test messages to each other, possibly accomplishing hole punching of any NATs»; при неудаче «communication can continue through (slower) relaying», причём попытки прямого соединения продолжаются бесконечно, есть port prediction для симметричного NAT и UPnP/NAT-PMP.

Разделение ролей, которое стоит запомнить: **roots** (facilitators транспортного уровня) ≠ **network controller** (CA и конфигурация сети). Network ID = 40-битный адрес контроллера + 24-битный номер сети.

Windows: собственный ребрендированный tap-windows6 — L2, потому в играх работает broadcast/ARP. Цена: GPLv2-драйвер и необходимость установки kernel-драйвера.
GUI: есть (трей-приложение). Переиспользование в Go: ядро C++, SDK `libzt` даёт C API (C#, Python, Rust, Java-биндинги), в Go — только через cgo; лицензия SDK — BSL-1.1, то есть для закрытого продукта требует коммерческой лицензии.

### 4.2 Tailscale — эталон control-plane и «правильный» WireGuard
README прямо говорит: в репозитории — `tailscaled` и CLI; GUI-обёртки для Windows/macOS закрыты. NAT traversal разобран в первоисточнике (`tailscale.com/blog/how-nat-traversal-works`): координационный сервер и флот **DERP**-серверов работают как side channel, пиры одновременно шлют UDP-пакеты навстречу, DERP — запасной путь, когда прямой не встаёт.

Для Go-проекта это самый богатый источник готовых решений: пакеты `wgengine`, `disco`, `derp`, `net/netns`, `control`, `tailcfg` и, главное, `tsnet` — встраиваемый узел Tailscale в произвольное Go-приложение. Лицензия BSD-3-Clause разрешает заимствование кода с сохранением копирайта.

### 4.3 Headscale — форма «свой сервер координации» на Go
Самописная реализация control-сервера Tailscale: одна tailnet, узкий скоуп, «exchange point of Wireguard public keys», раздаёт IP, границы между пользователями, advertised routes (`README.md`). Своего data plane нет — это чистый control plane, ровно та часть, которую предстоит писать нам. Показательно, что сервер написан на Go и живёт как один бинарник: это подтверждает реалистичность выбранного стека.

### 4.4 NetBird — самая точная схема «координация + обход NAT + релей»
README (`A bit on NetBird internals`) перечисляет ровно тот набор, который нужен MVP: агент управляет WireGuard; агент ходит в Management Service (состояние сети, IP пиров); кандидаты ищутся через STUN; «Agents use ICE (via pion/ice) to discover connection candidates»; обмен кандидатами идёт через Signal Service; при провале — «falls back to a Relay Service».

Это готовый чек-лист серверных компонентов. Ограничение: `management/`, `signal/`, `relay/` — AGPL-3.0, значит код этих каталогов нельзя брать в закрытый продукт; читать как спецификацию — можно.

### 4.5 Netmaker — «WireGuard-автоматизация» с самым удобным лицензированием
Компоненты по официальной документации: **Server** (центральный хаб конфигурации, пушит обновления), **Clients** (netclient для Windows/Linux/macOS/Docker, чистые WireGuard-endpoint'ы, Desktop/Mobile), **Message Queue** (синхронизация изменений), **DNS Service** (`learn.netmaker.io/getting-started/about.md`). NAT: «Netmaker leverages WireGuard, STUN, and TURN servers… As of v0.18.0, Netmaker uses a STUN server», список STUN задаётся через `STUN_LIST`. Есть TCP-uplink поверх WebSocket, когда UDP заблокирован, и Auto-Relay.

Ядро — Apache-2.0, это самый «дружелюбный» из полноценных платформ. Минус для нас: это платформа для сотен-тысяч узлов, с MQ, DNS-сервисом и UI — для 2–10 узлов избыточна.

### 4.6 Nebula — лучший код для чтения
«Mutually authenticated peer-to-peer software-defined network based on the Noise Protocol Framework». Discovery-узлы (**lighthouses**) позволяют пирам найти друг друга и «optionally use UDP hole punching to establish connections from behind most firewalls or NATs». Идентичность — сертификаты, в которых зашиты IP, имя и группы; ACL в стиле security groups. Центрального сервера нет, есть только lighthouse с публичным IP (в README предложен droplet за $6/мес).

MIT + компактный Go + Wintun в зависимостях = самый удобный источник архитектурных идей для чтения. Чего нет: модели «сеть + пароль» и обязательного релея «из коробки».

### 4.7 n2n — ближайшая по духу модель доступа, но C и GPL
README: обязательный **supernode** («must have a port publicly accessible on internet») + **edge**-узлы; сеть называется **community**, и в примерах запуска видно `-c mynetwork -k mysecretpass` — это буквально «имя сети + пароль», как в Radmin. Прямой UDP P2P, «when this is not possible (usually due to special NAT devices), the supernode is also used to relay the packets».

Ограничения: C, GPL-3.0, последний релиз 3.0 от 27.10.2021 при активных коммитах (то есть релизного цикла фактически нет 5 лет), официального GUI нет — только сторонний HappyNet.

### 4.8 tinc — минимализм и отсутствие ролей
«There is no real distinction between a server and a client in tinc» (`How-connections-work.html`). Данные идут UDP-туннелем, состояние — по meta-соединению (TCP); поддерживаются tun и tap, режимы router/switch/hub (`The-UDP-tunnel.html`). Идея «сеть сама строит туннели по списку участников» полезна, но для нашего MVP это лишняя децентрализация: нет места, где хранится пароль сети.

### 4.9 EasyTier — самый агрессивный NAT traversal
Заявлено: «Efficient NAT Traversal: Supports UDP and IPv6 traversal, works with NAT4-NAT4 networks» (NAT4 — симметричный NAT, худший случай), транспорты UDP/TCP/WSS/QUIC, шифрование AES-GCM или WireGuard, subnet proxy, web-консоль и GUI. Центральный сервер не обязателен: «EasyTier supports quick networking using shared public nodes… use the free shared nodes provided by the EasyTier community». В зависимостях — `stun_codec`, `quinn`, `boringtun`, форк `rust-tun` (`easytier/Cargo.toml`).

Это лучший источник идей по обходу симметричного NAT и по «relay через публичный узел», но язык — Rust, а LGPL-3.0 ограничивает заимствование кода.

### 4.10 SoftEther VPN — ближе всех по UX, дальше всех по стеку
README: Virtual Hub, SSL-VPN поверх HTTPS, «SSL-VPN Tunneling on HTTPS to pass through NATs and firewalls», «Ethernet-bridging (L2) and IP-routing (L3) over VPN», встроенный dynamic DNS и NAT-traversal. Плюс поддержка OpenVPN/IPsec/L2TP/SSTP/EtherIP. Windows-клиент имеет собственный виртуальный адаптер (официальный мануал, раздел «4.3 Virtual Network Adapter»), L2, и полноценный GUI-менеджер — то есть по ощущениям это и есть «Radmin, но с SSL-VPN».

Не подходит как образец кода: C, монолит на сотни тысяч строк, своя криптография и свой протокол; для MVP на Go это тупиковый путь.

### 4.11 innernet — полезный антипример
«Every innernet network needs a coordination server to manage peers and provide endpoint information so peers can directly connect to each other» (`README.md`). ACL строятся на CIDR, data plane — WireGuard, MIT, Rust. Но NAT traversal нет: если пир недостижим, соединения не будет. Ценность для нас — модель «сервер как раздатчик endpoint'ов и ACL», без которой не обойтись, и наглядная демонстрация, что одного этого недостаточно.

### 4.12 wg-access-server — не брать
Hub-and-spoke WireGuard с web-UI и регистрацией устройств по QR. Ни mesh, ни NAT traversal: нужен публичный адрес сервера. Последний коммит в `master` — **17.01.2021** (проверено через atom-фид коммитов), релизов нет; поле `pushed_at` в API показывает 2024 год, но это активность в других ветках. Проект фактически заброшен.

---

## 5. Малоизвестные клоны Radmin/Hamachi (результат дополнительного поиска)

| Проект | Ссылка | Язык | Лицензия | ★ | Активность | Суть |
|---|---|---|---|---|---|---|
| **Lanemu** | `gitlab.com/Monsterovich/lanemu` | Java | LGPL-3.0 | 84 | 26.10.2026 | Форк P2PVPN; **самый близкий к Radmin по UX** |
| FreeLAN | `github.com/freelan-developers/freelan` | C++ | не проверено | ~1.4k | не проверено | Старый «Hamachi-like» mesh VPN |
| Husarnet | `github.com/husarnet/husarnet` | C++ | не проверено | 378 | сентябрь 2026 | P2P VPN для IoT, свой координатор |
| webmesh | `github.com/webmeshproj/webmesh` | Go | не проверено | 466 | декабрь 2023 | Zero-config mesh на Go, заброшен |
| meshvpn | `github.com/VikeLabs/meshvpn` | Go | не проверено | 19 | ноябрь 2022 | «Control plane на Go + WireGuard», учебный |
| minilan-vpn | `github.com/Uss15/minilan-vpn` | не проверено | не проверено | 1 | октябрь 2026 | Заявлен как «Free Radmin/Hamachi-style» |
| orbitlan | `github.com/furqan-ahm/orbitlan` | не проверено | не проверено | 2 | октябрь 2026 | «Account-free virtual LAN», свежий |

**Lanemu — главная находка.** README: форк P2PVPN, «no central server is required», участники находят друг друга через **BitTorrent-трекер и DHT**, приглашения — подписанные RSA-4096 сертификаты (network certificate + access certificate), трафик — AES-128-GCM, Windows-интерфейс — TAP-Win32 / TAP-Windows V9 («Install the TAP-Win32 Adapter OAS driver by running `add_driver.bat`»). Ключевая фраза про NAT: «It's sufficient for at least one network member to have a port forwarded. If both network members are behind NAT, they can only connect through a third member».

То есть Lanemu закрывает UX (имя сети, приглашение, трей, Windows), но **не закрывает главную техническую проблему** — настоящий hole punching. Это ровно та ниша, где Radmin выигрывает, и ровно то, что предстоит реализовать в POSvpn.

Проекты на 1–2 звезды (minilan-vpn, orbitlan) зафиксированы как факт существования, но как образцы не рассматриваются: проверить их качество и архитектуру по одному коммиту невозможно.

---

## 6. Вывод: какие 2–3 проекта брать за образец

### Образец №1 — NetBird: схема разделения плоскостей
**Что копировать (решения, не код):**
1. Разделение на **Management** (кто в сети, какие IP, кто с кем может говорить), **Signal** (обмен кандидатами; состояние не хранит) и **Relay** (передача трафика, когда P2P не встал). Три сервиса можно собрать в один бинарник, но логически они обязаны быть разделены — иначе релей начнёт участвовать в согласовании.
2. **ICE-модель вместо «своего hole punching с нуля»**: кандидаты (host/srflx/relay) → обмен через сигнальный канал → проверка связности. Это проверенный путь, и в Go есть `pion/ice` + `pion/stun`.
3. **Правило деградации**: сначала пробуем прямую связь, при провале — релей, причём попытки прямого соединения не прекращаются.

**Чего не копировать:** AGPL-каталоги (`management/`, `signal/`, `relay/`) — только чтение; и полную модель IdP/SSO, которая противоречит требованию «вход по имени сети и паролю».

### Образец №2 — Headscale + Tailscale: control plane на Go
**Что копировать:**
1. **Сервер как единственный источник «карты сети»** (netmap): узел получает список пиров с ключами, endpoint'ами и разрешениями; любое изменение — новая версия карты. Это резко упрощает клиент: он не «договаривается» с другими клиентами, а исполняет выданную карту.
2. **Разделение ключей узла и ключей сессии**, выдача IP сервером, привязка узла к сети — у Headscale это сделано в одном Go-бинарнике, то есть масштаб кода сопоставим с нашим MVP.
3. **DERP-подобный релей как «домашний» путь**: узел всегда имеет запасной маршрут через релей, и клиент об этом знает.

**Чего не копировать:** disco-протокол Tailscale целиком (сложность ради сотен тысяч узлов), закрытые GUI-обёртки, обязательный логин через внешний IdP.

### Образец №3 — Nebula: структура компактного Go-кода
**Что копировать:**
1. **Lighthouse как «тонкий» координатор**: он не хранит состояние сети, а помогает пирам найти друг друга; вся авторизация — в сертификатах. Для 2–10 узлов это дешевле, чем полноценный control plane, и хорошо ложится на «сервер + SQLite».
2. **Идентичность в сертификате** (IP + имя + группы, подписаны CA) — так «имя сети» можно превратить в CA-контекст, а «пароль» — в право получить сертификат.
3. **Читаемая структура пакетов** (MIT, один бинарник, Wintun в зависимостях) — как образец того, как разложить наш код.

**Дополнительно, только для чтения:** EasyTier — единственный из списка, кто заявляет обход **NAT4–NAT4** (симметричный NAT) и делает relay через публичные узлы; его подход к классификации NAT и выбору транспорта стоит изучить, но реализовывать на Rust-идеях в Go нужно осторожно.

### Итоговая формула для POSvpn
`Headscale` (форма сервера: один Go-бинарник, карта сети, SQLite) + `NetBird` (разделение management/signal/relay и ICE на pion) + `Nebula` (сертификат как идентичность, lighthouse-подход, читаемость кода). Data plane — WireGuard через userspace `wireguard-go` + Wintun, а не собственный протокол.

---

## 7. Ниши, которые не закрывает ни один из проектов

1. **Вход по имени сети и паролю без аккаунта.** Tailscale требует логин, NetBird — настроенный IdP, Netmaker — API-ключи и сервер, Nebula и innernet — CA и раздачу сертификатов, ZeroTier — контроллер и авторизацию устройств. Модель «скачал, ввёл имя сети и пароль, попал в LAN» не реализована нигде, кроме n2n (но там нет GUI и релизов с 2021) и Lanemu (но там нет hole punching).
2. **L2-семантика для игр и старых приложений.** Radmin и Hamachi эмулируют Ethernet, поэтому работает broadcast/multicast/ARP и автообнаружение в играх. Из зрелых проектов L2 дают только ZeroTier, n2n (tap-режим), tinc (tap-режим) и SoftEther. Tailscale, NetBird, Netmaker, Nebula — L3/TUN: LAN-поиск в играх ломается. Это самая недооценённая ниша.
3. **Порог развёртывания.** Self-hosted NetBird/Netmaker — это Docker, DNS, TLS-сертификаты, IdP, TURN-сервер. Для 2–10 узлов это несоразмерно. Radmin не требует ничего — и именно поэтому выигрывает у технически лучших решений.
4. **Windows-первый продукт.** Трей-иконка, «создать сеть» / «подключиться», автозапуск, работа без администратора после установки, русский интерфейс. Из списка это есть у SoftEther (тяжёлый), ZeroTier (аккаунт), Lanemu (нет hole punching) — и всё.
5. **Приглашение как объект.** У Lanemu это файл-приглашение с подписью и сроком жизни — очень близко к тому, что нужно; но реализации «приглашение + настоящий P2P за NAT» в одном продукте нет.

---

## 8. Практические следствия (что это значит для кода)

| Решение | Обоснование из разбора |
|---|---|
| Data plane — WireGuard, не свой протокол | Так делают Tailscale, NetBird, Netmaker, innernet; в Go есть `wireguard-go`, на Windows — Wintun |
| Windows-интерфейс — **Wintun**, не TAP | Wintun не требует перезагрузки, L3, подписанные DLL; TAP (ZeroTier, n2n, tinc, Lanemu) — kernel-драйвер и GPLv2 |
| NAT traversal — ICE/STUN (`pion/ice`, `pion/stun`) + свой relay | Схема NetBird; собственный hole punching с нуля — месяцы работы и отладки на реальных NAT |
| Сервер — один Go-бинарник: карта сети + выдача ключей + релей | Форма Headscale; для 2–10 узлов SQLite достаточно |
| Идентичность — сертификат, выданный сервером после проверки «имя сети + пароль» | Идея Nebula (сертификат = IP + имя), но без ручного CA |
| L2 — не в MVP | L2 требует TAP-драйвера (GPLv2, подпись, перезагрузка) и сильно усложняет broadcast; вернуть позже отдельным режимом, если понадобятся игры |

**Лицензионная гигиена:** Wintun — исходники GPL-2.0, но «The prebuilt signed DLLs are released under a more permissive license than GPL 2.0, included in the ZIP archive» (`wintun.net`), то есть для закрытого продукта нужно брать именно подписанные DLL из архива и приложить их лицензию. Код NetBird из AGPL-каталогов, ZeroTier (MPL/BSL) и EasyTier (LGPL) — читать, не копировать. Tailscale (BSD-3), Nebula (MIT), Netmaker (Apache-2.0), innernet (MIT) — можно заимствовать с сохранением уведомлений.

---

## 9. Что не проверено

- Драйвер виртуального интерфейса **Netmaker** на Windows (в `go.mod` только `wgctrl`, Wintun не значится; в релизе — лишь `nmctl-windows-amd64.exe`).
- Использование **Wintun** в **EasyTier** — вывод сделан по форку `rust-tun`, а не по коду Windows-слоя.
- Драйвер **innernet** на Windows (предположительно официальный WireGuard-клиент).
- Детали TAP-режима **n2n** и **tinc** под Windows (по документации — tap-драйвер, по коду не сверял).
- Дата последнего коммита **FreeLAN** (получена только приблизительная оценка по shields.io, API был недоступен из-за лимита).
- Утверждение, что Radmin VPN эмулирует L2 — взято из описания продукта, первоисточник не открывался.
- Числа звёзд быстро устаревают: зафиксированы на 26.10.2026 через API.

---

## 10. Источники

**API и репозитории**
- `https://api.github.com/repos/{owner}/{repo}` — звёзды, язык, лицензия, `pushed_at` (12 репозиториев, 26.10.2026).
- `https://api.github.com/repos/{owner}/{repo}/commits?per_page=1` — дата последнего коммита.
- `https://api.github.com/repos/{owner}/{repo}/releases/latest` — последний релиз.
- `https://github.com/Place1/wg-access-server/commits/master.atom` — последний коммит 2021-01-17.
- `https://gitlab.com/api/v4/projects/Monsterovich%2Flanemu` — 84★, активность 26.10.2026.

**Файлы лицензий**
- `zerotier/ZeroTierOne/LICENSE.txt`, `LICENSE-MPL.txt`, `nonfree/LICENSE.md`.
- `netbirdio/netbird/LICENSE`.
- `gravitl/netmaker/LICENSE.md`.
- `gsliepen/tinc/COPYING` (GPL-2.0-or-later).
- `https://github.com/zerotier/libzt` — «ZeroTier and the ZeroTier SDK (libzt and libztcore) are licensed under the BSL version 1.1».
- `https://www.wintun.net/` — GPL-2.0 для исходников, более разрешительная лицензия для подписанных DLL.

**Документация и код**
- ZeroTier: `https://docs.zerotier.com/protocol/` (VL1/VL2, roots, rendezvous, hole punching, relay, moons); `ZeroTierOne/ext/bin/tap-windows-ndis6/x64/zttap300.inf` (строки 2–6: «ZeroTier One Virtual Network Port NDIS6 Driver… Based on the OpenVPN tap-windows6 driver version 9.21.1»).
- Tailscale: `https://tailscale.com/blog/how-nat-traversal-works` (STUN, координация, DERP); `tailscale/tailscale/go.mod` (`golang.zx2c4.com/wintun`).
- Headscale: `README.md` (control-сервер, одна tailnet, обмен публичными ключами).
- NetBird: `README.md`, раздел «A bit on NetBird internals» (Management, Signal, ICE/pion, STUN, Relay); `go.mod` (`wireguard/windows`, `pion/stun`).
- Netmaker: `https://learn.netmaker.io/getting-started/about.md` (Server, Clients, MQ, DNS); `https://learn.netmaker.io/how-to-guides/nat-traversal.md` (STUN с v0.18.0, TURN, STUN_LIST); `https://learn.netmaker.io/features/gateways/tcp-uplink.md`.
- Nebula: `slackhq/nebula/README.md` (Noise, lighthouses, UDP hole punching); `go.mod` (`golang.zx2c4.com/wintun`).
- n2n: `ntop/n2n/README.md` (supernode, community, `-k` ключ, relay).
- tinc: `https://www.tinc-vpn.org/documentation/How-connections-work.html`, `.../The-UDP-tunnel.html`.
- EasyTier: `EasyTier/EasyTier/README.md` (NAT4–NAT4, shared public nodes, транспорты); `easytier/Cargo.toml` (`tun-easytier`, `boringtun`, `stun_codec`, `quinn`).
- SoftEther: `SoftEtherVPN/SoftEtherVPN/README.md` (SSL-VPN over HTTPS/443, L2/L3, NAT-traversal, DDNS); `https://www.softether.org/4-docs/1-manual/4._softether_vpn_client_manual/4.3_virtual_network_adapter`.
- innernet: `tonarino/innernet/README.md` (coordination server, CIDR-ACL, WireGuard).
- wg-access-server: `Place1/wg-access-server/README.md` (hub-and-spoke, web-UI, gRPC-Web).
- Lanemu: `https://gitlab.com/Monsterovich/lanemu` (README: TAP-драйвер, BitTorrent-трекер/DHT, RSA-4096, ограничение hole punching); `LICENSE` (LGPL-3.0).
