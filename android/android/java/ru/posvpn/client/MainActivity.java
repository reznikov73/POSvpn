package ru.posvpn.client;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.Intent;
import android.net.VpnService;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.View;
import android.view.ViewGroup;
import android.widget.BaseAdapter;
import android.widget.ListView;
import android.widget.TextView;
import android.widget.Toast;

import org.json.JSONArray;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.List;

import ru.posvpn.core.mobile.Mobile;

/**
 * Главный экран: карточка состояния, кнопка ON/OFF и список участников
 * с задержкой. Статус опрашивается раз в секунду и подталкивается
 * коллбэками Go-движка.
 */
public class MainActivity extends Activity {

    private static final int REQUEST_VPN = 1;
    private static final long POLL_MS = 1000;

    private TextView tvState;
    private TextView tvNetwork;
    private TextView tvIp;
    private TextView tvNode;
    private TextView tvHub;
    private TextView tvError;
    private TextView tvPeersEmpty;
    private TextView btnToggle;
    private ListView listPeers;
    private PeersAdapter peers;
    private final Handler handler = new Handler(Looper.getMainLooper());
    private boolean toggling = false;

    private final Runnable pollTask = new Runnable() {
        @Override
        public void run() {
            refreshStatus();
            handler.postDelayed(this, POLL_MS);
        }
    };

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_main);

        Mobile.setDataDir(getFilesDir().getAbsolutePath());
        Mobile.setEvents(new ru.posvpn.core.mobile.Events() {
            @Override
            public void onStateChanged(final String stateJson) {
                handler.post(() -> refreshStatus());
            }

            @Override
            public void onNetmapUpdated(final String netmapJson) {
                handler.post(() -> refreshStatus());
            }
        });

        tvState = findViewById(R.id.tvState);
        tvNetwork = findViewById(R.id.tvNetwork);
        tvIp = findViewById(R.id.tvIp);
        tvNode = findViewById(R.id.tvNode);
        tvHub = findViewById(R.id.tvHub);
        tvError = findViewById(R.id.tvError);
        tvPeersEmpty = findViewById(R.id.tvPeersEmpty);
        btnToggle = findViewById(R.id.btnToggle);
        listPeers = findViewById(R.id.listPeers);
        peers = new PeersAdapter();
        listPeers.setAdapter(peers);

        btnToggle.setOnClickListener(v -> onToggle());
        findViewById(R.id.btnMenu).setOnClickListener(v -> showMenu());
    }

    @Override
    protected void onResume() {
        super.onResume();
        refreshStatus();
        handler.postDelayed(pollTask, POLL_MS);
    }

    @Override
    protected void onPause() {
        super.onPause();
        handler.removeCallbacks(pollTask);
    }

    private void onToggle() {
        if (toggling) {
            return;
        }
        if (!Mobile.hasConfig()) {
            new AlertDialog.Builder(this)
                    .setTitle(R.string.no_network_title)
                    .setMessage(R.string.no_network_text)
                    .setPositiveButton(R.string.settings, (d, w) ->
                            startActivity(new Intent(this, SettingsActivity.class)))
                    .setNegativeButton(R.string.no, null)
                    .show();
            return;
        }

        JSONObject status = parse(Mobile.status());
        boolean running = status != null && status.optBoolean("running", false);
        if (running) {
            toggling = true;
            PosVpnService.stop(this);
            handler.postDelayed(() -> toggling = false, 800);
            return;
        }

        // Разрешение VPN ещё не выдано — спрашиваем у системы.
        Intent prepare = VpnService.prepare(this);
        if (prepare != null) {
            startActivityForResult(prepare, REQUEST_VPN);
            return;
        }
        PosVpnService.start(this);
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode == REQUEST_VPN) {
            if (resultCode == RESULT_OK) {
                PosVpnService.start(this);
            } else {
                Toast.makeText(this, R.string.no_network_title,
                        Toast.LENGTH_SHORT).show();
            }
        }
    }

    private void showMenu() {
        android.widget.PopupMenu menu = new android.widget.PopupMenu(this, findViewById(R.id.btnMenu));
        menu.getMenu().add(R.string.settings);
        menu.getMenu().add(R.string.leave_network);
        menu.getMenu().add(R.string.about);
        menu.setOnMenuItemClickListener(item -> {
            String title = String.valueOf(item.getTitle());
            if (title.equals(getString(R.string.settings))) {
                startActivity(new Intent(this, SettingsActivity.class));
            } else if (title.equals(getString(R.string.leave_network))) {
                confirmLeave();
            } else {
                new AlertDialog.Builder(this)
                        .setTitle(R.string.app_name)
                        .setMessage(R.string.about_text)
                        .setPositiveButton(R.string.yes, null)
                        .show();
            }
            return true;
        });
        menu.show();
    }

    private void confirmLeave() {
        new AlertDialog.Builder(this)
                .setTitle(R.string.leave_network)
                .setMessage(R.string.leave_confirm)
                .setPositiveButton(R.string.yes, (d, w) -> {
                    PosVpnService.stop(this);
                    new Thread(() -> {
                        final Exception[] error = {null};
                        try {
                            Mobile.leave();
                        } catch (Exception e) {
                            error[0] = e;
                        }
                        handler.post(() -> {
                            if (error[0] != null) {
                                Toast.makeText(this,
                                        error[0].getMessage(), Toast.LENGTH_LONG).show();
                            }
                            refreshStatus();
                        });
                    }).start();
                })
                .setNegativeButton(R.string.no, null)
                .show();
    }

    private void refreshStatus() {
        JSONObject status = parse(Mobile.status());
        JSONObject info = parse(Mobile.configInfo());
        if (status == null) {
            return;
        }

        String state = status.optString("state", "down");
        boolean running = status.optBoolean("running", false);
        String error = status.optString("error", "");

        String stateText;
        int color;
        switch (state) {
            case "up":
                stateText = getString(R.string.state_up);
                color = getColor(R.color.online);
                break;
            case "wait":
                stateText = getString(R.string.state_waiting);
                color = getColor(R.color.text_secondary);
                break;
            case "error":
                stateText = getString(R.string.state_error);
                color = getColor(R.color.error);
                break;
            default:
                stateText = getString(running ? R.string.state_waiting : R.string.state_down);
                color = getColor(R.color.text_secondary);
                break;
        }
        tvState.setText(stateText);
        tvState.setTextColor(color);

        String network = status.optString("network", "");
        if (network.isEmpty() && info != null) {
            network = info.optString("network", "");
        }
        tvNetwork.setText(network.isEmpty() ? getString(R.string.placeholder) : network);
        tvIp.setText(orPlaceholder(status.optString("self_ip", "")));
        tvNode.setText(orPlaceholder(status.optString("node", "")));
        tvHub.setText(info != null ? orPlaceholder(info.optString("hub", "")) : getString(R.string.placeholder));

        if (error.isEmpty()) {
            tvError.setVisibility(View.GONE);
        } else {
            tvError.setVisibility(View.VISIBLE);
            tvError.setText(error);
        }

        btnToggle.setText(running ? R.string.btn_disconnect : R.string.btn_connect);
        btnToggle.setEnabled(Mobile.hasConfig() || running);

        JSONArray peerArray = status.optJSONArray("peers");
        peers.replace(peerArray);
        boolean empty = peerArray == null || peerArray.length() == 0;
        tvPeersEmpty.setVisibility(empty && Mobile.hasConfig() ? View.VISIBLE : View.GONE);
    }

    private String orPlaceholder(String value) {
        return value == null || value.isEmpty() ? getString(R.string.placeholder) : value;
    }

    private static JSONObject parse(String json) {
        try {
            return new JSONObject(json);
        } catch (Exception e) {
            return null;
        }
    }

    /** Список участников: имя, адрес, online-точка и RTT. */
    private class PeersAdapter extends BaseAdapter {
        private final List<JSONObject> items = new ArrayList<>();

        void replace(JSONArray peers) {
            items.clear();
            if (peers != null) {
                for (int i = 0; i < peers.length(); i++) {
                    JSONObject peer = peers.optJSONObject(i);
                    if (peer != null) {
                        items.add(peer);
                    }
                }
            }
            notifyDataSetChanged();
        }

        @Override
        public int getCount() {
            return items.size();
        }

        @Override
        public Object getItem(int position) {
            return items.get(position);
        }

        @Override
        public long getItemId(int position) {
            return position;
        }

        @Override
        public View getView(int position, View convertView, ViewGroup parent) {
            View view = convertView;
            if (view == null) {
                view = getLayoutInflater().inflate(R.layout.item_peer, parent, false);
            }
            JSONObject peer = items.get(position);

            TextView name = view.findViewById(R.id.tvPeerName);
            TextView ip = view.findViewById(R.id.tvPeerIp);
            TextView rtt = view.findViewById(R.id.tvPeerRtt);
            View dot = view.findViewById(R.id.dotOnline);

            String peerName = peer.optString("name", "");
            String role = peer.optString("role", "user");
            if ("admin".equals(role)) {
                peerName += " · " + getString(R.string.role_admin);
            }
            name.setText(peerName);
            ip.setText(peer.optString("ip", ""));

            boolean online = peer.optBoolean("online", false);
            dot.setBackgroundResource(online ? R.drawable.dot_online : R.drawable.dot_offline);

            if (online) {
                long ms = peer.optLong("rtt_ms", 0);
                rtt.setText(ms > 0 ? ms + " мс" : getString(R.string.peer_online));
                rtt.setTextColor(getColor(R.color.online));
            } else {
                rtt.setText(R.string.peer_offline);
                rtt.setTextColor(getColor(R.color.text_secondary));
            }
            return view;
        }
    }
}
