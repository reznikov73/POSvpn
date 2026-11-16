package ru.posvpn.client;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.net.VpnService;
import android.os.ParcelFileDescriptor;
import android.util.Log;

import org.json.JSONObject;

import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.RejectedExecutionException;

import ru.posvpn.core.mobile.Mobile;

/**
 * Служба туннеля POSvpn. Создаёт TUN-адаптер через VpnService.Builder,
 * передаёт его дескриптор Go-движку и живёт с постоянным уведомлением,
 * пока туннель поднят.
 *
 * По умолчанию маршрут только на 10.77.0.0/16 — split-tunneling, интернет
 * телефона идёт напрямую. Переключатель «Весь трафик через VPN» в
 * настройках добавляет маршруты 0.0.0.0/1 + 128.0.0.0/1 и DNS — тогда
 * трафик уходит в сеть POSvpn и выходит в интернет через шлюз с NAT.
 *
 * Служба не заворачивает в туннель саму себя (addDisallowedApplication):
 * иначе её же запросы к координатору и хабу уходят в собственный TUN.
 * Вся работа с движком идёт в отдельном потоке — Mobile.start() синхронно
 * ходит в сеть, а главный поток службы общий с UI.
 */
public class PosVpnService extends VpnService {

    private static final String TAG = "PosVpnService";

    public static final String ACTION_START = "ru.posvpn.client.START";
    public static final String ACTION_STOP = "ru.posvpn.client.STOP";
    public static final int MTU = 1100; // tunnel.DefaultMTU

    /** Имя настроек и ключ флага «весь трафик через VPN». */
    public static final String PREFS = "posvpn";
    public static final String KEY_ROUTE_ALL = "route_all";

    private static final String CHANNEL_ID = "posvpn-tunnel";
    private static final int NOTIFICATION_ID = 1;

    /**
     * Единственный рабочий поток службы. Mobile.start() синхронно запрашивает
     * карту сети у координатора (apiclient.go: fetchNetMap, таймаут 15 с), а
     * onStartCommand выполняется на главном потоке процесса — вместе с UI.
     */
    private ExecutorService worker;

    /** Поля состояния читает и пишет только рабочий поток worker. */
    private boolean tunnelUp;
    private boolean routeAllApplied;
    private volatile boolean destroyed;

    private SharedPreferences prefs;
    private SharedPreferences.OnSharedPreferenceChangeListener prefListener;

    /** Читает флаг «весь трафик через VPN» (по умолчанию выключен). */
    public static boolean isRouteAll(android.content.Context ctx) {
        return ctx.getSharedPreferences(PREFS, MODE_PRIVATE)
                .getBoolean(KEY_ROUTE_ALL, false);
    }

    public static void start(MainActivity activity) {
        Intent intent = new Intent(activity, PosVpnService.class);
        intent.setAction(ACTION_START);
        activity.startService(intent);
    }

    public static void stop(MainActivity activity) {
        Intent intent = new Intent(activity, PosVpnService.class);
        intent.setAction(ACTION_STOP);
        activity.startService(intent);
    }

    @Override
    public void onCreate() {
        super.onCreate();
        createChannel();
        worker = Executors.newSingleThreadExecutor();

        // Переключатель «весь трафик через VPN» меняет маршруты TUN, а они
        // задаются только в establish(). Поэтому поднятый туннель
        // пересобирается сразу, иначе флаг молча ждёт переподключения.
        prefs = getSharedPreferences(PREFS, MODE_PRIVATE);
        prefListener = (p, key) -> {
            if (KEY_ROUTE_ALL.equals(key)) {
                post(this::rebuildForRouteChange);
            }
        };
        prefs.registerOnSharedPreferenceChangeListener(prefListener);
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String action = intent != null ? intent.getAction() : null;
        if (intent == null || ACTION_STOP.equals(action)) {
            // intent == null — система перезапустила службу после гибели
            // процесса: TUN и состояние движка потеряны, продолжать нечего.
            post(this::stopTunnel);
            return START_NOT_STICKY;
        }
        post(this::startTunnel);
        return START_NOT_STICKY;
    }

    /** Ставит задачу в рабочий поток; после onDestroy задачи отбрасываются. */
    private void post(Runnable task) {
        try {
            worker.execute(task);
        } catch (RejectedExecutionException ignored) {
            // Служба уже остановлена — работа не нужна.
        }
    }

    /**
     * Поднимает TUN и передаёт дескриптор движку. Только рабочий поток:
     * Mobile.start() синхронно ходит за картой сети к координатору.
     */
    private void startTunnel() {
        if (destroyed) {
            return;
        }
        if (tunnelUp) {
            if (engineRunning()) {
                return; // уже поднят
            }
            // Движок завершил сессию и закрыл дескриптор — поднимаем заново.
            tunnelUp = false;
        }

        String selfIp = "10.77.0.1";
        try {
            JSONObject info = new JSONObject(Mobile.configInfo());
            if (info.has("ip") && !info.isNull("ip") && !info.optString("ip").isEmpty()) {
                selfIp = info.getString("ip");
            }
        } catch (Exception ignored) {
        }

        boolean routeAll = isRouteAll(this);
        Builder builder = new Builder()
                .setSession(getString(R.string.app_name))
                .addAddress(selfIp, 24)
                .addRoute("10.77.0.0", 16)
                .setMtu(MTU)
                .setBlocking(true);

        if (routeAll) {
            // Полный туннель: два /1 покрывают весь IPv4-пространство
            // (0.0.0.0/0 VpnService.Builder не принимает).
            // Трафик уйдёт к шлюзу сети POSvpn — на нём должен быть NAT.
            builder.addRoute("0.0.0.0", 1);
            builder.addRoute("128.0.0.0", 1);
            builder.addDnsServer("8.8.8.8");
            builder.addDnsServer("1.1.1.1");
        }

        // Собственные сокеты приложения должны идти мимо туннеля: по умолчанию
        // VpnService заворачивает в него все приложения, включая это, и запрос
        // карты сети (:8787) и QUIC к хабу (:8788) на 10.77.1.1 попадают в
        // собственный TUN, где их некому доставить. Метод доступен с API 21,
        // minSdk 26; пакет всегда установлен.
        try {
            builder.addDisallowedApplication(getPackageName());
        } catch (PackageManager.NameNotFoundException e) {
            Log.w(TAG, "не удалось исключить приложение из туннеля: " + e.getMessage());
        }

        ParcelFileDescriptor pfd = builder.establish();
        if (pfd == null) {
            // Пользователь отозвал разрешение VPN или система отказала.
            stopSelf();
            return;
        }

        // Владение дескриптором переходит к движку: fdDevice.Close()
        // (android/go/mobile/device.go) закрывает его сам. detachFd отсоединяет
        // дескриптор от ParcelFileDescriptor, поэтому закрывать его в Java
        // повторно нельзя — иначе двойной close одного и того же fd.
        int fd = pfd.detachFd();
        routeAllApplied = routeAll;

        startForeground(NOTIFICATION_ID, buildNotification());
        try {
            Mobile.start(fd, MTU);
            tunnelUp = true;
        } catch (Exception e) {
            // Движок не взял дескриптор (нет входа в сеть, координатор
            // недоступен): закрываем его здесь — Java остаётся владельцем.
            Log.w(TAG, "движок не запущен: " + e.getMessage());
            closeQuietly(fd);
            stopForeground(true);
            stopSelf();
        }
    }

    /** Останавливает туннель и снимает уведомление. Только рабочий поток. */
    private void stopTunnel() {
        stopEngine();
        stopForeground(true);
        stopSelf();
    }

    /**
     * Останавливает движок. Дескриптор TUN закрывает Go-сторона
     * (fdDevice.Close в конце сессии) — в Java его не трогаем.
     */
    private void stopEngine() {
        Mobile.stop();
        tunnelUp = false;
        routeAllApplied = false;
    }

    /**
     * Пересобирает туннель после смены флага «весь трафик через VPN»:
     * маршруты задаются только при establish(). stop и start идут
     * последовательно в одном потоке и не перекрываются.
     */
    private void rebuildForRouteChange() {
        if (destroyed || !tunnelUp) {
            return; // туннель не поднят — флаг применится при подключении
        }
        if (isRouteAll(this) == routeAllApplied) {
            return;
        }
        stopEngine();
        startTunnel();
    }

    /** Спрашивает движок, жива ли сессия: локальный JSON, без сети. */
    private static boolean engineRunning() {
        try {
            return new JSONObject(Mobile.status()).optBoolean("running", false);
        } catch (Exception e) {
            return false;
        }
    }

    /** Закрывает дескриптор, который движок не забрал. */
    private static void closeQuietly(int fd) {
        try {
            ParcelFileDescriptor.adoptFd(fd).close();
        } catch (Exception ignored) {
        }
    }

    @Override
    public void onRevoke() {
        // Система отозвала разрешение (например, запущен другой VPN).
        post(this::stopTunnel);
    }

    @Override
    public void onDestroy() {
        destroyed = true;
        if (prefs != null && prefListener != null) {
            prefs.unregisterOnSharedPreferenceChangeListener(prefListener);
        }
        if (worker != null) {
            // Гасим движок после текущей задачи и не принимаем новые.
            post(this::stopEngine);
            worker.shutdown();
        }
        super.onDestroy();
    }

    private Notification buildNotification() {
        Intent open = new Intent(this, MainActivity.class);
        PendingIntent pending = PendingIntent.getActivity(
                this, 0, open,
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);

        return new Notification.Builder(this, CHANNEL_ID)
                .setSmallIcon(R.drawable.ic_shield)
                .setContentTitle(getString(R.string.notification_title))
                .setContentText(getString(R.string.notification_text))
                .setContentIntent(pending)
                .setOngoing(true)
                .build();
    }

    private void createChannel() {
        NotificationManager nm = getSystemService(NotificationManager.class);
        if (nm != null) {
            NotificationChannel channel = new NotificationChannel(
                    CHANNEL_ID,
                    getString(R.string.notification_channel),
                    NotificationManager.IMPORTANCE_LOW);
            nm.createNotificationChannel(channel);
        }
    }
}
