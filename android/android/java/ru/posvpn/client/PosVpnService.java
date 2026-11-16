package ru.posvpn.client;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Intent;
import android.net.VpnService;
import android.os.ParcelFileDescriptor;

import org.json.JSONObject;

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
 */
public class PosVpnService extends VpnService {

    public static final String ACTION_START = "ru.posvpn.client.START";
    public static final String ACTION_STOP = "ru.posvpn.client.STOP";
    public static final int MTU = 1100; // tunnel.DefaultMTU

    /** Имя настроек и ключ флага «весь трафик через VPN». */
    public static final String PREFS = "posvpn";
    public static final String KEY_ROUTE_ALL = "route_all";

    private static final String CHANNEL_ID = "posvpn-tunnel";
    private static final int NOTIFICATION_ID = 1;

    private ParcelFileDescriptor tun;

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
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String action = intent != null ? intent.getAction() : ACTION_START;
        if (ACTION_STOP.equals(action)) {
            stopTunnel();
            return START_NOT_STICKY;
        }
        startTunnel();
        return START_STICKY;
    }

    private void startTunnel() {
        if (tun != null) {
            return; // уже поднят
        }

        String selfIp = "10.77.0.1";
        try {
            JSONObject info = new JSONObject(Mobile.configInfo());
            if (info.has("ip") && !info.isNull("ip") && !info.optString("ip").isEmpty()) {
                selfIp = info.getString("ip");
            }
        } catch (Exception ignored) {
        }

        Builder builder = new Builder()
                .setSession(getString(R.string.app_name))
                .addAddress(selfIp, 24)
                .addRoute("10.77.0.0", 16)
                .setMtu(MTU)
                .setBlocking(true);

        if (isRouteAll(this)) {
            // Полный туннель: два /1 покрывают весь IPv4-пространство
            // (0.0.0.0/0 VpnService.Builder не принимает).
            // Трафик уйдёт к шлюзу сети POSvpn — на нём должен быть NAT.
            builder.addRoute("0.0.0.0", 1);
            builder.addRoute("128.0.0.0", 1);
            builder.addDnsServer("8.8.8.8");
            builder.addDnsServer("1.1.1.1");
        }

        tun = builder.establish();
        if (tun == null) {
            // Пользователь отозвал разрешение VPN или система отказала.
            stopSelf();
            return;
        }

        startForeground(NOTIFICATION_ID, buildNotification());
        try {
            Mobile.start(tun.getFd(), MTU);
        } catch (Exception e) {
            // Go-сторона не смогла поднять туннель: не беда, уведомляем
            // пользователя и аккуратно закрываемся.
            stopForeground(true);
            try { tun.close(); } catch (Exception ignored) {}
            tun = null;
            stopSelf();
        }
    }

    private void stopTunnel() {
        Mobile.stop();
        if (tun != null) {
            try {
                tun.close();
            } catch (Exception ignored) {
            }
            tun = null;
        }
        stopForeground(true);
        stopSelf();
    }

    @Override
    public void onRevoke() {
        // Система отозвала разрешение (например, запущен другой VPN).
        stopTunnel();
    }

    @Override
    public void onDestroy() {
        if (tun != null) {
            Mobile.stop();
            try {
                tun.close();
            } catch (Exception ignored) {
            }
            tun = null;
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
