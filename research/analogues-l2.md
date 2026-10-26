# L2-VPN и клоны Hamachi/Radmin: у кого настоящий виртуальный Ethernet

**Срез данных:** 26.10.2026.
**Вопрос:** какие открытые проекты дают именно L2 (Ethernet-кадры, broadcast, LAN-игры), а не только IP-маршрутизацию; как они обходят NAT; какой драйвер ставят на Windows; есть ли центральный сервер; живы ли они.
**Границы:** только L2-срез. Общая таблица аналогов и модель доступа Radmin — в соседних файлах `research/analogues.md` и `research/radmin-features.md` (собраны в тот же день, другими задачами); здесь они используются как перекрёстная проверка, а не как первоисточник.

**Как читать уровни доказательств:**
**[П]** — прочитано в первоисточнике (код, документация проекта, GitHub API/Atom-фид) в этот заход.
**[П-втор]** — первоисточник, но вторичный по природе (сторонний reverse-engineering, README чужого проекта).
**[Г]** — мой вывод из перечисленных фактов, не цитата.
**«не проверено»** — не подтверждал, в conclusions не опираюсь.

---

## 1. Главное

1. **Настоящий L2 в открытом коде есть ровно у четырёх живых проектов: n2n, tinc, SoftEther VPN и ZeroTier.** Все четыре на Windows работают только через NDIS-минпорт (TAP-подобный или собственный) — пользовательского способа получить Ethernet-интерфейс в Windows нет. **[П]**
2. **OpenVPN — не L2-решение по умолчанию.** TAP-режим у него есть (`--dev tap`), но в 2.7 поддержку wintun **удалили**, а на Windows TAP-режим держится на `tap-windows6`. То есть OpenVPN — это «L2, если поставить сторонний драйвер», а не готовый виртуальный коммутатор. **[П]**
3. **ZeroTier — единственный, кто сделал L2 массовым продуктом:** VL2 «эмуляция Ethernet, похожая на VXLAN», свой форк TAP-драйвера `zttap300` на основе tap-windows6 9.21.1. Но контроллер сети лежит в каталоге `nonfree/` (source-available, не open source). **[П]**
4. **Wintun — по конструкции L3** и бродкаст не отдаёт: API возвращает «pointer to layer 3 IPv4 or IPv6 packet», Ethernet-заголовка и ARP там нет by design. Обойти можно только эмуляцией на уровне IP (релей UDP-broadcast), что делает EasyTier флагом `enable_udp_broadcast_relay`. **[П]**
5. **Клоны Hamachi/Radmin, которые находятся поиском, — это либо L3-надстройки (iroh-lan, partylan), либо проекты на 1–20 звёзд.** Зрелого открытого L2-клона Radmin нет; ближайшие по функциональности — n2n и ZeroTier, но у них другая модель доступа. **[П]**
6. **Для вашего MVP (Wintun, Go, 2–10 узлов) честная развилка такая:** либо L3 + релей UDP-broadcast (работает для части LAN-игр, дешево), либо отдельный трек на `tap-windows6` (настоящий L2, но GPLv2 + подпись драйвера). Смешать нельзя: адаптер либо L2, либо L3. **[Г]**

---

## 2. Сводная таблица

Звёзды и даты — GitHub search API и Atom-фиды релизов, сняты **26.10.2026**. Где число расходится с `research/analogues.md`, расхождение указано в разделе 8.

| Проект | Репозиторий | Язык | Лицензия | ★ | Последний релиз (дата) | Уровень | NAT | Драйвер Windows | Центр. сервер | Состояние |
|---|---|---|---|---|---|---|---|---|---|---|
| **n2n** | ntop/n2n | C | GPL-3.0 | 7 046 | 3.1.1 (31.10.2021) | **L2** (TAP) | P2P UDP, при неудаче relay через supernode | tap-windows (OpenVPN) | supernode, поднимается сам | живой: коммиты 17.10.2026, релизов нет с 2021 |
| **tinc** | gsliepen/tinc | C | GPL-2.0-or-later | 2 259 | dev-снапшот `latest` (01.03.2026); до него 1.1pre18 (2019) | **L2** (switch mode) + L3 (router) | mesh, `ConnectTo`, трафик через промежуточный узел | TAP-Win32 (OpenVPN) | нет, равноправные демоны | живой, но 1.1 так и не выпущена |
| **SoftEther VPN** | SoftEtherVPN/SoftEtherVPN | C | Apache-2.0 | 13 673 | stable 4.44-9807-rtm (07.05.2025); dev 5.2.5188 (18.07.2025) | **L2** (Virtual Hub = Ethernet-коммутатор) | SSL-VPN поверх HTTPS/443 + NAT-T, VPN Azure как fallback | собственный виртуальный NIC | Virtual Hub на своём сервере | живой |
| **OpenVPN** | OpenVPN/openvpn | C | GPL-2.0 | 14 729 | 2.7.8 (07.10.2026) | **L2 только в `--dev tap`** | нет traversal: нужен открытый порт или TCP/443 | tap-windows6 (dco-win по умолчанию, wintun удалён в 2.7) | свой сервер | живой |
| **ZeroTier One** | zerotier/ZeroTierOne | C++ | MPL-2.0 + `nonfree/` | 17 211 | 1.16.2 (28.05.2026) | **L2** (VL2) | root-серверы → hole punching, port prediction, relay | свой `zttap300` (форк tap-windows6 9.21.1) | контроллер можно свой (`nonfree/`) | живой |
| **vpncloud** | dswd/vpncloud | Rust | GPL-3.0 | 1 977 | v2.3.0 (23.12.2021) | **L2** (TAP) + L3 | mesh, без центральных серверов | **нет порта под Windows** | нет | заморожен (README просит портировать) |
| **fastd** | neocturne/fastd | C | BSD-2-Clause | 151 | v23 (26.01.2025) | **L2/L3** («IP packets and Ethernet frames») | 1:1, 1:n, mesh | **нет Windows** (Linux/BSD/macOS) | нет | живой, нишевый (Freifunk) |
| **EasyTier** | EasyTier/EasyTier | Rust | LGPL-3.0 | 14 350 | v2.6.4 (02.05.2026) | **L3** + релей UDP-broadcast | P2P, заявлен NAT4–NAT4, публичные узлы | Wintun | не обязателен | очень живой |
| **iroh-lan** | rustonbsd/iroh-lan | Rust | MIT | 228 | — (релизы есть, дату не снимал) | **L3** (сам пишет «private L3 LAN») | iroh (relay-инфраструктура) | wintun.dll | нет | живой, молодой |
| **tap-windows6** | OpenVPN/tap-windows6 | C | GPLv2 | 904 | 9.27.0 (19.03.2024) | **L2** (это и есть драйвер) | — | — | — | maintenance |

---

## 3. Карточки: что именно подтверждено

### 3.1 n2n — самый близкий по духу «открытый Hamachi»

- Модель: `supernode` + `edge`. Supernode «allows edge nodes to announce and discover other nodes. It must have a port publicly accessible on internet»; сеть называется community. **[П]** — README, <https://github.com/ntop/n2n>.
- L2 подтверждается тем, что в заголовке пакета фигурирует «the virtual MAC address of the edge nodes»: узлы обмениваются Ethernet-кадрами, а не IP-пакетами. **[П]** — тот же README, раздел Security Considerations.
- NAT: «n2n tries to establish a direct peer-to-peer connection via udp between the edge nodes when possible. When this is not possible… the supernode is also used to relay the packets». **[П]** — README, абзац About.
- Windows: официальной сборки нет, но есть рецепт сборки, и он прямо требует TAP-драйверы: «The TAP drivers should be installed into the system. They can be installed from http://build.openvpn.net/downloads/releases, search for "tap-windows"». **[П]** — `research/_raw/l2/n2n-Building.md:84`.
- Состояние: последний релиз — тег **3.1.1, 31.10.2021** (Atom-фид релизов), при этом коммиты в `dev` есть и в 2026 (последний 17.10.2026). То есть проект поддерживается, но релизного цикла нет. **[П]**
- Вердикт для вашей задачи: единственный живой проект с моделью «сеть по имени + ключ» и L2 «из коробки» (на Linux). На Windows цена — TAP-драйвер. Код на C, GPL-3.0 — как библиотеку в Go-продукт не встроить.

### 3.2 tinc — mesh без сервера, switch mode = настоящий Ethernet

- «Unicast, multicast and broadcast packets of every protocol that runs over Ethernet are supported in this mode» — это описание switch mode; там же «dynamically create a routing table just like an Ethernet switch does». **[П]** — `research/_raw/l2/tinc-master/doc/tinc.texi:1009`.
- Там же: «any protocol that runs over Ethernet is supported (for instance IPX and Appletalk)» — то, чего L3-туннель не даёт в принципе. **[П]** — `tinc.texi:2026`.
- Windows: «You will need to install the latest TAP-Win32 driver from OpenVPN». **[П]** — `tinc.texi:314`.
- NAT: центрального сервера нет; узлы соединяются по `ConnectTo`, при недоступности — трафик идёт через промежуточный узел mesh. **[П]** — `tinc.texi:689–705` (описание поведения при `ConnectTo`).
- Состояние: 1.0.37 — актуальный стабильный тег; ветка 1.1 (SPTPS) в релиз так и не вышла: последний `1.1pre18` — 2019, поверх него dev-снапшот 01.03.2026. **[П]**
- Вердикт: технически самый «чистый» L2, но модель доверия другая — нет места, где хранится пароль сети; конфиги и ключи раздаются вручную. Для «вход по имени и паролю» не подходит как есть.

### 3.3 SoftEther VPN — эталон L2-архитектуры

- «SoftEther means Software Ethernet»; «SoftEther VPN Client implements Virtual Network Adapter, and SoftEther VPN Server implements Virtual Ethernet Switch… as expansion of Ethernet-based L2 VPN». **[П]** — <https://www.softether.org/4-docs/1-manual/3._SoftEther_VPN_Protocol>.
- Как устроено внутри: «The Virtual Hub exchanges all Ethernet packets from each connected VPN session to other connected sessions. The behavior is same to traditional Ethernet switches. The Virtual Hub has a FDB (forwarding database)». **[П]** — там же. Это готовое описание того, что вам придётся писать для L2-режима.
- NAT: «A VPN session is established by SoftEther VPN's "VPN over HTTPS" technology… beyond any kinds of firewalls and NATs»; плюс UDP-режим с NAT traversal. **[П]** — там же.
- Драйвер: свой виртуальный адаптер (компонент VPN Client), не tap-windows6. **[П]** — по документации адаптер «software-emulated»; **тип драйвера (NDIS-минпорт) в этот заход отдельно не подтверждал** — не проверено.
- Лицензия Apache-2.0, язык C, 13 673★, коммиты 20.10.2026. **[П]**
- Вердикт: лучший источник архитектурных решений (Virtual Hub + FDB + Local Bridge), но это монолит на C с своим стеком, а не библиотека.

### 3.4 OpenVPN в TAP-режиме — «L2, но не бесплатно по сложности»

- `--dev tun` — IP, `--dev tap` — Ethernet; это разные типы устройств, и они несовместимы между пирами. **[П]** — `research/_raw/l2/openvpn-master/doc/man-sections/generic-options.rst:185` («…where one peer uses `--dev tun` while the other peer uses `--dev tap`»).
- В 2.7 **поддержку wintun удалили**: «OpenVPN 2.7 now drops the support for wintun driver… In all other cases OpenVPN will fall back automatically to tap-windows6 driver». **[П]** — `research/_raw/l2/openvpn-master/Changes.md:246–253`.
- Это важно для вас: OpenVPN не «пример использования Wintun», а пример того, что для L2 всё равно нужен tap-windows6.
- NAT: своего traversal нет; нужен доступный порт (или TCP/443 как обходной путь). **[П]** — общая модель client/server в документации; отдельной цитаты про traversal не снимал — **не проверено**.
- Вердикт: как L2-транспорт — рабочий, но это star-topology VPN, а не «виртуальная локалка» с обнаружением. Плюс GPLv2 и внешний драйвер.

### 3.5 ZeroTier — L2 как продукт

- «…combining a cryptographically addressed and secure peer-to-peer network (termed VL1) with an Ethernet emulation layer somewhat similar to VXLAN (termed VL2)». **[П]** — README, <https://github.com/zerotier/ZeroTierOne>.
- Relay: «Most traffic flows peer-to-peer, though we offer free (but slow) relaying for users who cannot establish peer-to-peer connections». **[П]** — там же.
- Windows-драйвер: свой, `zttap300`, ставится из INF через `UpdateDriverForPlugAndPlayDevicesA`; в коде — `tapDriverName = "zttap300"`. **[П]** — `research/_raw/l2/zt-wintun.go:92` (сохранённый `osdep/WindowsEthernetTap.cpp`). Соседний отчёт по INF даёт точную родословную: «Based on the OpenVPN tap-windows6 driver version 9.21.1» (`research/analogues.md`, строка про ZeroTier).
- Лицензия: основное — MPL-2.0, но «See nonfree/LICENSE.md for all non-free ("source available") portions». **[П]** — `research/_raw/l2/zt-LICENSE.txt:1–5`.
- Контроллер: «This is our reference controller implementation and is almost the same as the one we use to power our own hosted services… Controller data is stored in JSON format under `controller.d`». То есть **свой контроллер поднять можно**, но код лежит в `nonfree/`. **[П]** — `nonfree/controller/README.md`.
- Вердикт: единственный, у кого L2-модель, self-hosted контроллер и зрелый Windows-клиент существуют одновременно. Но воспроизводить их путь = писать свой NDIS-драйвер (см. §6).

### 3.6 Мёртвые и нерелевантные L2-проекты (проверено, чтобы не искать заново)

| Проект | Факт | Уровень |
|---|---|---|
| vpncloud | «It can work on TUN devices (IP based) and TAP devices (Ethernet based)», режимы Hub/Switch/Router, «no central servers». Но: «If you are a Rust developer with experience on Windows or MacOS your help on porting VpnCloud to those platforms is very…» — порта под Windows нет. Релиз v2.3.0 от 23.12.2021. | **[П]** `research/_raw/l2/vpncloud-README.md:6,29,65–66` |
| fastd | «tunnels IP packets and Ethernet frames over UDP», «runs on Linux, FreeBSD, OpenBSD and macOS» — Windows нет. v23 (26.01.2025), BSD-2-Clause. | **[П]** `research/_raw/l2/fastd-main/README.md:3,5`; `fastd-main/LICENSE` |
| PeerVPN | Сайт peervpn.net отвечает 200, но оригинального репозитория на GitHub нет: поиск даёт только форки 2012–2015 годов (`i-md/PeerVPN` 9★, последний push 2012). Считать заброшенным. | **[П]** поиск GitHub API + HTTP-код сайта |
| IPOP | `github.com/ipop-project/ipop` отдаёт 404; в организации остался только `ipop-project/Downloads` (30★, 2020). Проект фактически недоступен. | **[П]** HTTP 404 |
| fishman/n2n, bambusoft/ntvl, NaixuC/Simple-L2-VPN | Старые/мелкие L2-поделки: 26★/2010, 24★/2020, 3★ (Linux TAP+UDP). Для продукта непригодны. | **[П]** метаданные GitHub |

---

## 4. Малоизвестные «клоны»: что реально нашлось

Поиск по запросам `hamachi`, `radmin vpn`, `virtual lan internet`, `lan party vpn`, `layer 2 vpn` (GitHub search API, сортировка по звёздам, 26.10.2026). Полная выдача — шум; значимые попадания:

| Репозиторий | ★ | Стек | Что заявлено | Что на самом деле |
|---|---|---|---|---|
| rustonbsd/iroh-lan | 228 | Rust, MIT | «iroh-lan = hamachi - account - install» | **L3**: «private L3 LAN», «UDP/TCP/ICMP at L3», на Windows нужен `wintun.dll` **[П]** `research/_raw/l2/iroh-lan-README.md:27,73,122` |
| gyf304/lpvpn | 294 | C++, GPL-3.0 | «LAN Party VPN — Run VPN on top of Discord» | Помечен **Deprecated**: Discord закрыл Game SDK; преемник — `partylan`, тоже на Wintun **[П]** `research/_raw/l2/lpvpn-README.md:1–4`; `partylan-README.md:31` |
| baptisterajaut/radmin-vpn-linux | 265 | C, GPL-3.0 | Radmin VPN под Wine | Не VPN, а шим: подменяет NDIS-драйвер Radmin на TAP-мост. Ценен как описание внутренностей Radmin (см. §5) |
| gringoestrangeiro/openrad | 17 | Java (в описании «Rust») | «Open-source Radmin VPN reimplementation» | Молодой, описание противоречит языку — **не проверено** |
| wdcrhzs-create/OpenLAN | 4 | C | «virtual LAN across the public Internet» | Один автор, без релизов — **не проверено** |
| codedpro/dotachi | 2 | Go | «Self-hosted LAN Gaming Network» | Игрушечный масштаб |
| alvarogabrielgomez/kanpachi | 2 | Go, AGPL-3.0 | «virtual private LAN for gaming» | То же |
| dev-nolant/PeerDen | 1 | C++, MIT | «P2P virtual LAN for gaming» | То же |
| tvladyslav/vpnparty | 8 | Rust, GPL-3.0 | «LAN party via VPN» | То же |

**Вывод по поиску:** зрелого открытого L2-клона Radmin/Hamachi нет. Всё, что моложе и звёзднее, — L3 с релеем broadcast (iroh-lan, partylan) либо обёртки вокруг чужого драйвера. Дополнительно в `research/analogues.md` упомянуты **Lanemu** (GitLab, Java-форк P2PVPN) и **minilan-vpn** — я их сам не проверял, это указатель, а не подтверждение.

---

## 5. Что технически нужно, чтобы приложение «увидело локальную сеть»

Это не про VPN-протокол, а про пять независимых условий. Пропуск любого ломает сценарий, даже если туннель работает и `ping` идёт.

1. **Ethernet-семантика интерфейса.** Приложению и стеку нужен адаптер с MAC-адресом, ARP и MTU 1500, а не point-to-point IP-интерфейс. Radmin это делает буквально: INF драйвера объявляет `*IfType = 0x6 ; IF_TYPE_ETHERNET_CSMACD`, `*MediaType = 0x0 ; NdisMedium802_3`, `UpperRange = "ndis5"`, `LowerRange = "ethernet"` — то есть система видит Ethernet-адаптер. **[П]** `research/radmin-features.md:171` (разбор INF установщика Radmin).
2. **Доставка кадров на broadcast/multicast MAC всем узлам.** Кадр на `FF:FF:FF:FF:FF:FF` и multicast-группы (`01:00:5E:…`, `33:33:…`) должен уходить всем пирам сети — это функция коммутатора: flood неизвестного unicast + broadcast + multicast, с обучения FDB и защитой от петель. У SoftEther это Virtual Hub с FDB, у tinc — switch mode с «routing table just like an Ethernet switch», у n2n — relay через supernode. **[П]** (§3.2, §3.3).
3. **Не-IP протоколы.** Старые игры используют IPX/SPX, кое-что — NetBEUI/AppleTalk. Это EtherType, а не IP; L3-туннель их не переносит вообще. tinc прямо указывает: «any protocol that runs over Ethernet is supported (for instance IPX and Appletalk)». **[П]** `tinc.texi:2026`.
4. **Маршрутизация broadcast/multicast в Windows на виртуальный адаптер.** По умолчанию Windows отправит `255.255.255.255` и `224.0.0.0/4` в физический интерфейс. В Radmin это решает сам драйвер: «On Windows the Radmin TAP driver advertises itself as the preferred interface for those flows». На Linux то же делается руками: `ip route append 255.255.255.255/32 dev radminvpn0`, `ip route append 224.0.0.0/4 dev radminvpn0`. **[П-втор]** `github.com/baptisterajaut/radmin-vpn-linux` (README, раздел про LAN games) — сторонний RE-проект, но с конкретными командами и объяснением.
5. **Сетевой профиль и firewall.** Обнаружение SMB/USB-принтеров/медиа — это входящие multicast/broadcast-запросы (WS-Discovery, SSDP, NetBIOS-NS), которые Windows режет, если сеть помечена как «Общественная» или выключено «Обнаружение сети». Даже идеальный L2 не поможет, пока профиль адаптера не Private и не разрешены правила группы «Network Discovery». **[Г]** — это следствие модели профилей Windows; отдельной цитаты в этот заход не снимал — **не проверено**.

### 5.1 Почему Wintun этого не даёт

- Wintun — «a **layer 3** TUN driver for Windows 7, 8, 8.1, 10, and 11… intended to be useful to a wide variety of projects that require **layer 3** tunneling devices». **[П]** — <https://git.zx2c4.com/wintun/about/>.
- В API то же самое: `WintunReceivePacket` возвращает «Pointer to **layer 3 IPv4 or IPv6** packet», `WintunAllocateSendPacket` — «memory where to prepare **layer 3 IPv4 or IPv6** packet». **[П]** — там же, раздел Reference.
- Следствия, которых нельзя обойти настройкой:
  - нет Ethernet-заголовка → нет MAC-адресов пиров и нет ARP-ответов от имени соседей; всё, что ищет соседа через ARP (часть старых игр, некоторые реализации SMB/NetBIOS), не работает **[Г]**;
  - нет доставки не-IP EtherType (IPX) **[П]** — прямо следует из «layer 3 IPv4 or IPv6»;
  - multicast-группы на L2 не эмулируются: приложение «вступает» в группу на физическом интерфейсе, а не в оверлее **[Г]**.
- Что **можно** сделать на Wintun: увидеть в TUN IP-пакет с адресом `255.255.255.255` или направленный broadcast подсети и размножить его по всем пирам в userspace. Это ровно то, что делает EasyTier: в коде есть `enable_udp_broadcast_relay` (**[П]** `research/_raw/l2/EasyTier-main/easytier-core/src/config/api_input.rs:87`) и отдельный модуль классификации broadcast (`easytier-core/src/gateway/udp_broadcast.rs`, типы `PhysicalInterface`/`directed_broadcast`). Работает для игр, которые кричат UDP-broadcast'ом; **не работает** для ARP-only discovery, IPX и всего, что требует Ethernet-кадра.
- Ограничение честно: это не «виртуальная локальная сеть», а «L3-оверлей с релеем broadcast». Разница вылезет на первой игре, которая ищет хоста через ARP или шлёт IPX.

---

## 6. Легальные способы получить L2 на Windows в 2026

Пользовательского (userspace-only) пути нет: чтобы приложения видели Ethernet-интерфейс, в системе должен появиться NDIS-минпорт 802.3. Вариантов три с половиной.

### 6.1 tap-windows6 (OpenVPN) — единственный готовый

- Что это: NDIS 6.20/6.30 TAP-драйвер, L2, ставится как обычный сетевой адаптер. Версия **9.27.0**, тег 19.03.2024, последний push 19.03.2024, 904★. **[П]** — `version.m4` (`PRODUCT_VERSION 9.27.0`), Atom-фид релизов.
- Лицензия **GPLv2**. **[П]** — `COPYING` в репозитории, начало файла.
- Подпись: у OpenVPN уже есть подписанные сборки; но если вы собираете свой вариант — нужна **EV-подпись + attestation-подпись через Partner Center**: «You must have an extended validation (EV) code signing certificate… Submit the EV signed CAB file in Partner Center». **[П]** — <https://learn.microsoft.com/en-us/windows-hardware/drivers/dashboard/code-signing-attestation>.
- Нюанс из того же документа: «Attestation signed drivers that target retail audiences aren't published on Windows Update» — то есть attestation годится для распространения своим установщиком, но не заменяет WHQL/HLK, если нужен Windows Update. **[П]**
- Цена для продукта: (а) GPLv2 — при распространении бинарника драйвера вы попадаете под обязательства GPL (исходники/предложение исходников); это вопрос к юристу, не техническое решение; (б) установка драйвера = UAC; (в) драйвер 2024 года и не развивается.

### 6.2 Свой NDIS 6 L2-минпорт

- Технически это то, что делают Famatech (Radmin, `RvNetMP60.sys`, INF `NetMP60_1_1_*`) и SoftEther (свой виртуальный адаптер). **[П]** для Radmin — `research/radmin-features.md:113,171`; для SoftEther — по документации (§3.3), тип драйвера не проверял.
- Порог входа: драйвер + INF + `.cat`, EV-сертификат, HLK-прогоны для WHQL, attestation для распространения. Месяцы работы и постоянная поддержка при обновлениях Windows. **[Г]** на основе требований Microsoft (§6.1).
- Когда оправдано: если L2 — продуктовое ядро (LAN-игры как главный сценарий), а не галочка.

### 6.3 Сторонние драйверы и обходные пути

| Вариант | Почему не то | Уровень |
|---|---|---|
| Npcap | Это packet capture (фильтр), а не виртуальный адаптер: приложения не увидят новый интерфейс как сеть. OEM-лицензия. | **[Г]**, лицензию в этот заход не читал — не проверено |
| WinpkFilter | Коммерческий NDIS-фильтр, тоже не создаёт Ethernet-адаптер для приложений. | **[Г]** |
| Hyper-V Extensible Switch / VXLAN (Windows Server) | Требует Hyper-V и серверной роли; не годится для бытового клиента. | **[Г]** |
| «Wintun + релей broadcast» | Не L2: см. §5.1 — годится для UDP-broadcast игр, не для ARP/IPX. | **[П]** по механике EasyTier |
| Свой драйвер на базе форка tap-windows6 | Наследует GPLv2 → те же обязательства, плюс своя подпись. Так сделал ZeroTier (`zttap300`). | **[П]** |

**Итог §6:** легально и бесплатно — только `tap-windows6` (GPLv2, чужая подпись) либо «Wintun + эмуляция broadcast». Всё остальное требует своей подписи драйвера и денег.

---

## 7. Что это значит для вашего MVP

1. **Не переписывайте транспорт под L2 «на всякий случай».** Ваш текущий выбор (Wintun + свой оверлей + свой координатор) совпадает с тем, что делают EasyTier и iroh-lan — оба сознательно L3. Это валидная архитектура, а не компромисс.
2. **Добавьте релей broadcast как отдельную функцию, а не как режим.** Ориентир — EasyTier: флаг `enable_udp_broadcast_relay`, классификация `255.255.255.255` и направленного broadcast, пересылка всем пирам сети. Это дешево (десятки строк на Go) и закрывает часть LAN-игр.
3. **Проверьте целевые игры до выбора драйвера.** Если они ищут хоста UDP-broadcast'ом — Wintun+релей хватит. Если ARP/IPX — нужен `tap-windows6`, и это отдельный трек с GPLv2 и UAC.
4. **Не берите OpenVPN как референс L2:** в 2.7 он сам ушёл от wintun к tap-windows6, а traversal у него нет.
5. **Если нужен именно «как Radmin»** — архитектура известна: L2-минпорт + MAC-based forwarding + relay/flood, виртуальная подсеть внутри выделенного диапазона (у Radmin — `26.0.0.0/8`), служба под SYSTEM + GUI отдельно. **[П-втор]** `radmin-vpn-linux`; **[П]** `research/radmin-features.md:207`.

---

## 8. Расхождения с соседними отчётами (не прячу)

| Что | В `research/analogues.md` | У меня | Комментарий |
|---|---|---|---|
| n2n, последний релиз | «3.0 (27.10.2021)» | тег **3.1.1, 31.10.2021** | Мой источник — Atom-фид релизов ntop/n2n; 3.1.1 — более поздний тег, «3.0 Stable Release» — предыдущий |
| EasyTier, звёзды | 14 349 | 14 350 | Разные моменты запроса 26.10.2026; расхождение в 1 звезду |
| EasyTier, дата релиза | v2.6.4 (12.05.2026) | v2.6.4 (02.05.2026 по Atom) | Возможно, дата коммита vs дата публикации релиза |
| tinc, релизы | «релизов на GitHub нет (теги)» | Релизы есть: тег `latest` (dev, 01.03.2026) и `release-1.1pre18` (2019) | GitHub-релизы tinc существуют, но 1.1 так и не выпущена |

Это не ошибки соседей, а разные срезы/источники; для чисел в этой таблице я опираюсь на Atom-фиды и search API, снятые 26.10.2026.

---

## 9. Не проверено (и как проверять)

1. **Тип драйвера SoftEther** (NDIS-минпорт или иное) — по документации только «virtual network adapter». Проверять: распаковать установщик SoftEther VPN Client, прочитать INF.
2. **Npcap/WinpkFilter: условия redistribution** — не читал лицензии; для продукта это отдельный юридический вопрос.
3. **Профиль сети и firewall для SMB-обнаружения** — вывод из модели Windows, не эксперимент. Проверять: две VM, Wintun-оверлей, `Get-NetConnectionProfile`, включение «Network Discovery», тест `Get-SmbConnection`/обнаружения.
4. **Поведение конкретных LAN-игр** (broadcast vs ARP vs IPX) — не тестировал; это решает выбор драйвера, поэтому проверять до кода.
5. **Attestation vs WHQL для «retail»-драйвера** — в документе Microsoft есть фраза про отсутствие публикации в Windows Update; как это влияет на установку своим MSI, надо уточнять в актуальной политике подписи.
6. **ZeroTier root-инфраструктура** (сколько root-серверов, можно ли полностью заменить своими) — в этот заход не смотрел `docs.zerotier.com`; в отчёте указано только то, что подтверждено README и `nonfree/controller/README.md`.

---

## 10. Источники

**Первоисточники кода/документации (скачаны и прочитаны 26.10.2026):**
- `research/_raw/l2/n2n-Building.md` — ntop/n2n, doc/Building.md (Windows: TAP-драйверы).
- `research/_raw/l2/tinc-master/doc/tinc.texi` — switch mode (1009–1013), IPX/Appletalk (2026), TAP-Win32 (314–317), `ConnectTo` (689–705).
- `research/_raw/l2/openvpn-master/doc/man-sections/generic-options.rst:185`; `Changes.md:246–253` (удаление wintun в 2.7).
- `research/_raw/l2/vpncloud-README.md:6,29,65–66`; `vpncloud-master/LICENSE.md` (GPL-3); `Cargo.toml` (2.4.0-dev).
- `research/_raw/l2/fastd-main/README.md:3,5`; `fastd-main/LICENSE` (BSD-2-Clause).
- `research/_raw/l2/EasyTier-main/easytier-core/src/config/api_input.rs:87`; `.../gateway/udp_broadcast.rs`; `Cargo.toml` (smoltcp).
- `research/_raw/l2/zt-wintun.go` — сохранённый `zerotier/ZeroTierOne/osdep/WindowsEthernetTap.cpp` (строки 82–93: `zttap300`).
- `research/_raw/l2/zt-LICENSE.txt:1–5` (MPL-2.0 + `nonfree/`).
- `research/_raw/l2/iroh-lan-README.md:27,73,122`; `partylan-README.md:31`; `lpvpn-README.md:1–4`.

**Веб-первоисточники:**
- <https://www.softether.org/4-docs/1-manual/3._SoftEther_VPN_Protocol> — L2, Virtual Hub, FDB, Ethernet over HTTPS (прочитано 26.10.2026).
- <https://git.zx2c4.com/wintun/about/> — «layer 3 TUN driver», формат пакета (26.10.2026).
- <https://learn.microsoft.com/en-us/windows-hardware/drivers/dashboard/code-signing-attestation> — EV + Partner Center + CAB (26.10.2026).
- <https://github.com/zerotier/ZeroTierOne/blob/dev/nonfree/controller/README.md> — self-hosted контроллер.
- <https://github.com/baptisterajaut/radmin-vpn-linux> — RE-описание драйвера Radmin, broadcast-маршруты (вторичный источник).

**Метаданные:** GitHub search API (`/search/repositories`) и Atom-фиды `releases.atom`/`tags.atom`/`commits.atom`, сняты 26.10.2026. Значения звёзд: n2n 7 046; tinc 2 259; SoftEther 13 673; OpenVPN 14 729; ZeroTier 17 211; vpncloud 1 977; fastd 151; EasyTier 14 350; tap-windows6 904; iroh-lan 228; lpvpn 294; radmin-vpn-linux 265.

**Перекрёстные (внутренние) источники:** `research/analogues.md` (общая таблица аналогов), `research/radmin-features.md` (разбор установщика Radmin: INF, `RvNetMP60.sys`, `IF_TYPE_ETHERNET_CSMACD`/`NdisMedium802_3`), `research/go-stack.md` (Wintun/tap-windows6/подпись — срезано тем же днём).
