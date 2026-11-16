package ru.posvpn.client;

import android.app.Activity;
import android.content.SharedPreferences;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.TextUtils;
import android.view.View;
import android.widget.Button;
import android.widget.EditText;
import android.widget.Switch;
import android.widget.Toast;

import org.json.JSONObject;

import ru.posvpn.core.mobile.Mobile;

/**
 * Настройки: адрес координатора, хаб, отпечаток, имя сети, пароль и имя
 * устройства. Отсюда же создаются сети и выполняется вход.
 *
 * Весь onCreate обёрнут в try-catch: при любой ошибке (например, сбой
 * инициализации Go-движка) активность не падает, а показывает Toast с
 * точной причиной — это помогает диагностировать краши на реальных
 * устройствах без logcat.
 */
public class SettingsActivity extends Activity {

    private EditText etServer;
    private EditText etHub;
    private EditText etPin;
    private EditText etNetwork;
    private EditText etPassword;
    private EditText etNode;
    private EditText etGateway;
    private Button btnCreate;
    private Button btnJoin;
    private Button btnSaveServer;
    private Switch swRouteAll;
    private final Handler handler = new Handler(Looper.getMainLooper());

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        try {
            setContentView(R.layout.activity_settings);
        } catch (Throwable t) {
            // Если layout не раздувается — дальше идти нельзя.
            reportAndFinish("layout: " + t.getMessage());
            return;
        }

        try {
            etServer = findViewById(R.id.etServer);
            etHub = findViewById(R.id.etHub);
            etPin = findViewById(R.id.etPin);
            etNetwork = findViewById(R.id.etNetwork);
            etPassword = findViewById(R.id.etPassword);
            etNode = findViewById(R.id.etNode);
            etGateway = findViewById(R.id.etGateway);
            btnCreate = findViewById(R.id.btnCreate);
            btnJoin = findViewById(R.id.btnJoin);
            btnSaveServer = findViewById(R.id.btnSaveServer);
            swRouteAll = findViewById(R.id.swRouteAll);
        } catch (Throwable t) {
            reportAndFinish("findViewById: " + t.getMessage());
            return;
        }

        try {
            prefill();
        } catch (Throwable t) {
            // prefill не критичен — пользователь введёт всё вручную.
            toast("prefill: " + t.getMessage());
        }

        if (btnCreate != null) {
            btnCreate.setOnClickListener(v -> runAuth(true));
        }
        if (btnJoin != null) {
            btnJoin.setOnClickListener(v -> runAuth(false));
        }
        if (btnSaveServer != null) {
            btnSaveServer.setOnClickListener(v -> saveServer());
        }

        // Переключатель «весь трафик через VPN»: сохраняется сразу,
        // применяется при следующем подключении туннеля.
        if (swRouteAll != null) {
            SharedPreferences prefs = getSharedPreferences(PosVpnService.PREFS, MODE_PRIVATE);
            swRouteAll.setChecked(prefs.getBoolean(PosVpnService.KEY_ROUTE_ALL, false));
            swRouteAll.setOnCheckedChangeListener((btn, checked) -> {
                try {
                    getSharedPreferences(PosVpnService.PREFS, MODE_PRIVATE)
                            .edit()
                            .putBoolean(PosVpnService.KEY_ROUTE_ALL, checked)
                            .apply();
                    toast(getString(checked
                            ? R.string.route_all_on_toast
                            : R.string.route_all_off_toast));
                } catch (Throwable t) {
                    toast("prefs: " + t.getMessage());
                }
            });
        }
    }

    private void reportAndFinish(String reason) {
        new android.app.AlertDialog.Builder(this)
                .setTitle(R.string.error_title)
                .setMessage(getString(R.string.error_settings_open) + "\n\n" + reason)
                .setOnDismissListener(d -> finish())
                .show();
    }

    private void toast(String msg) {
        if (msg == null || msg.isEmpty()) return;
        try {
            Toast.makeText(this, msg, Toast.LENGTH_LONG).show();
        } catch (Throwable ignored) {
        }
    }

    /** Предзаполнение полей из текущих настроек Go-движка. */
    private void prefill() {
        try {
            JSONObject info = new JSONObject(Mobile.configInfo());
            if (info.has("server")) {
                etServer.setText(info.optString("server", ""));
            }
            if (info.has("hub")) {
                etHub.setText(info.optString("hub", ""));
            }
            if (info.has("hub_pin")) {
                etPin.setText(info.optString("hub_pin", ""));
            }
            if (info.has("network")) {
                etNetwork.setText(info.optString("network", ""));
            }
            if (info.has("node")) {
                etNode.setText(info.optString("node", ""));
            }
            if (info.has("gateway")) {
                etGateway.setText(info.optString("gateway", ""));
            }
        } catch (Throwable ignored) {
            // Конфига ещё нет — это нормальное состояние при первом запуске.
        }
        if (etNode.getText().length() == 0) {
            String model = android.os.Build.MODEL != null
                    ? sanitize(android.os.Build.MODEL) : "phone";
            etNode.setText(model);
        }
    }

    /** Модель телефона как имя узла: только буквы, цифры, дефис и подчёркивание. */
    static String sanitize(String name) {
        StringBuilder sb = new StringBuilder();
        for (char c : name.toCharArray()) {
            if (Character.isLetterOrDigit(c) || c == '-' || c == '_') {
                sb.append(c);
            }
        }
        String result = sb.toString().trim();
        return result.isEmpty() ? "phone" : result;
    }

    private void runAuth(final boolean create) {
        final String server = etServer.getText().toString().trim();
        final String hub = etHub.getText().toString().trim();
        final String pin = etPin.getText().toString().trim();
        final String network = etNetwork.getText().toString().trim();
        final String password = etPassword.getText().toString();
        final String node = etNode.getText().toString().trim();
        final String gateway = etGateway.getText().toString().trim();

        if (TextUtils.isEmpty(network)) {
            etNetwork.setError(getString(R.string.network_label));
            return;
        }
        if (password.length() < 8) {
            etPassword.setError(getString(R.string.password_label));
            return;
        }

        setBusy(true);
        new Thread(() -> {
            final Exception[] error = {null};
            try {
                // Шлюз сохраняем до входа: он прочитается при старте туннеля.
                Mobile.setGateway(gateway);
                if (create) {
                    Mobile.create(server, hub, pin, network, password, node);
                } else {
                    Mobile.join(server, hub, pin, network, password, node);
                }
            } catch (Exception e) {
                error[0] = e;
            }
            handler.post(() -> {
                setBusy(false);
                if (error[0] != null) {
                    Toast.makeText(SettingsActivity.this,
                            error[0].getMessage(), Toast.LENGTH_LONG).show();
                } else {
                    Toast.makeText(SettingsActivity.this,
                            create ? R.string.btn_create : R.string.btn_join,
                            Toast.LENGTH_SHORT).show();
                    finish();
                }
            });
        }).start();
    }

    private void saveServer() {
        final String server = etServer.getText().toString().trim();
        final String hub = etHub.getText().toString().trim();
        final String pin = etPin.getText().toString().trim();
        final String gateway = etGateway.getText().toString().trim();
        new Thread(() -> {
            final Exception[] error = {null};
            try {
                Mobile.setServer(server, hub, pin);
                Mobile.setGateway(gateway);
            } catch (Exception e) {
                error[0] = e;
            }
            handler.post(() -> {
                if (error[0] == null) {
                    Toast.makeText(SettingsActivity.this,
                            R.string.server_saved, Toast.LENGTH_SHORT).show();
                } else {
                    Toast.makeText(SettingsActivity.this,
                            error[0].getMessage(), Toast.LENGTH_LONG).show();
                }
            });
        }).start();
    }

    private void setBusy(boolean busy) {
        if (btnCreate != null) btnCreate.setEnabled(!busy);
        if (btnJoin != null) btnJoin.setEnabled(!busy);
        if (btnSaveServer != null) btnSaveServer.setEnabled(!busy);
    }
}
