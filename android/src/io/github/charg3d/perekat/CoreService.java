package io.github.charg3d.perekat;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.pm.ServiceInfo;
import android.database.Cursor;
import android.net.Uri;
import android.provider.OpenableColumns;
import android.content.pm.ShortcutInfo;
import android.content.pm.ShortcutManager;
import android.graphics.drawable.Icon;
import android.media.Ringtone;
import android.media.RingtoneManager;
import android.net.ConnectivityManager;
import android.net.LinkProperties;
import android.net.Network;
import android.net.NetworkRequest;
import android.net.wifi.WifiManager;
import android.os.BatteryManager;
import android.os.Build;
import android.os.Environment;
import android.os.Handler;
import android.os.IBinder;
import android.os.Looper;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.File;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.Inet4Address;
import java.net.InterfaceAddress;
import java.net.NetworkInterface;
import java.net.URLEncoder;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/**
 * Keeps the daemon running. The daemon is the same program as on a computer,
 * started as a separate process; this service does for it what only an
 * Android application can: the clipboard, notifications, sounds, the battery.
 */
public class CoreService extends Service {
    private static final String TAG = "Perekat";
    private static final String CHANNEL_SERVICE = "service";
    private static final String CHANNEL_EVENTS = "events";
    static final String ACTION_SHARE = "io.github.charg3d.perekat.SHARE";

    /** Address of the interface, set once the daemon is up. */
    static volatile String url;
    /** Why the daemon could not start, if it could not. */
    static volatile String error;
    private static volatile String base;
    private static volatile String token;

    private Process daemon;
    private WifiManager.MulticastLock multicast;
    private volatile boolean running = true;
    private final Handler main = new Handler(Looper.getMainLooper());
    private int nextNotification = 100;

    @Override
    public void onCreate() {
        super.onCreate();
        NotificationManager manager = getSystemService(NotificationManager.class);
        manager.createNotificationChannel(new NotificationChannel(
                CHANNEL_SERVICE, getString(R.string.channel_service), NotificationManager.IMPORTANCE_LOW));
        manager.createNotificationChannel(new NotificationChannel(
                CHANNEL_EVENTS, getString(R.string.channel_events), NotificationManager.IMPORTANCE_DEFAULT));

        Notification status = new Notification.Builder(this, CHANNEL_SERVICE)
                .setSmallIcon(android.R.drawable.stat_notify_sync_noanim)
                .setContentTitle(getString(R.string.running))
                .setContentIntent(openApp())
                .setOngoing(true)
                .build();
        if (Build.VERSION.SDK_INT >= 34) {
            startForeground(1, status, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE);
        } else {
            startForeground(1, status);
        }

        WifiManager wifi = (WifiManager) getApplicationContext().getSystemService(Context.WIFI_SERVICE);
        multicast = wifi.createMulticastLock("perekat");
        multicast.acquire();

        new Thread(this::runDaemon, "daemon").start();
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null && ACTION_SHARE.equals(intent.getAction())) {
            new Thread(() -> share(intent), "share").start();
        }
        return START_STICKY;
    }

    /** Sends what was shared from another application, showing how it goes. */
    private void share(Intent work) {
        String label = work.getStringExtra("label");
        String folder = work.getStringExtra("folder");
        String text = work.getStringExtra("text");
        List<Uri> files = work.getParcelableArrayListExtra("files");
        NotificationManager manager = getSystemService(NotificationManager.class);
        int id = nextNotification++;
        Notification.Builder progress = new Notification.Builder(this, CHANNEL_SERVICE)
                .setSmallIcon(android.R.drawable.stat_sys_upload)
                .setContentTitle(getString(R.string.share_sending, label))
                .setOngoing(true)
                .setOnlyAlertOnce(true);
        String result;
        try {
            if (!waitReady(8000)) throw new Exception(getString(R.string.not_ready));
            String device = "?device=" + URLEncoder.encode(work.getStringExtra("device"), "UTF-8");
            if (files == null || files.isEmpty()) {
                call("POST", "text" + device, JSONObject.quote(text), 15000);
            } else {
                long total = 0;
                for (Uri file : files) total += Math.max(0, sizeOf(file));
                manager.notify(id, progress.setProgress(100, 0, total == 0).build());
                String path = folder == null ? "upload" + device : "fs/upload" + device + "&dir=" + URLEncoder.encode(folder, "UTF-8");
                upload(path, files, total, (sent, all) ->
                        manager.notify(id, progress.setProgress(100, (int) (sent * 100 / Math.max(1, all)), all == 0)
                                .setContentText(nameOf(files.get(0)) + (files.size() > 1 ? " +" + (files.size() - 1) : "")).build()));
            }
            result = getString(R.string.share_done, label);
        } catch (Exception e) {
            Log.w(TAG, "share", e);
            result = getString(R.string.share_failed, String.valueOf(e.getMessage()));
        }
        manager.notify(id, new Notification.Builder(this, CHANNEL_EVENTS)
                .setSmallIcon(android.R.drawable.stat_sys_upload_done)
                .setContentTitle(result)
                .setContentIntent(openApp())
                .setAutoCancel(true)
                .build());
    }

    private interface Progress {
        void at(long sent, long total);
    }

    /** Streams files to the daemon, which passes them on. */
    private void upload(String path, List<Uri> files, long total, Progress progress) throws Exception {
        String boundary = "perekat" + System.nanoTime();
        HttpURLConnection connection = open("POST", path);
        try {
            connection.setReadTimeout(0);
            connection.setDoOutput(true);
            connection.setChunkedStreamingMode(64 * 1024);
            connection.setRequestProperty("Content-Type", "multipart/form-data; boundary=" + boundary);
            try (OutputStream out = connection.getOutputStream()) {
                byte[] buffer = new byte[64 * 1024];
                long sent = 0;
                long shown = 0;
                for (Uri file : files) {
                    String name = nameOf(file).replace("\"", "'").replace("\r", "").replace("\n", "");
                    out.write(("--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"" + name
                            + "\"\r\nContent-Type: application/octet-stream\r\n\r\n").getBytes(StandardCharsets.UTF_8));
                    try (InputStream in = getContentResolver().openInputStream(file)) {
                        if (in == null) throw new Exception(name);
                        for (int n; (n = in.read(buffer)) > 0; ) {
                            out.write(buffer, 0, n);
                            sent += n;
                            if (System.currentTimeMillis() - shown > 500) {
                                shown = System.currentTimeMillis();
                                progress.at(sent, total);
                            }
                        }
                    }
                    out.write("\r\n".getBytes(StandardCharsets.UTF_8));
                }
                out.write(("--" + boundary + "--\r\n").getBytes(StandardCharsets.UTF_8));
            }
            if (connection.getResponseCode() != 200) throw new Exception(read(connection.getErrorStream()));
        } finally {
            connection.disconnect();
        }
    }

    private String nameOf(Uri file) {
        try (Cursor cursor = getContentResolver().query(file, new String[]{OpenableColumns.DISPLAY_NAME}, null, null, null)) {
            if (cursor != null && cursor.moveToFirst() && cursor.getString(0) != null) return cursor.getString(0);
        } catch (Exception ignored) {
            // Not every source tells the name.
        }
        String last = file.getLastPathSegment();
        return last == null ? "file" : last.substring(last.lastIndexOf('/') + 1);
    }

    private long sizeOf(Uri file) {
        try (Cursor cursor = getContentResolver().query(file, new String[]{OpenableColumns.SIZE}, null, null, null)) {
            if (cursor != null && cursor.moveToFirst() && !cursor.isNull(0)) return cursor.getLong(0);
        } catch (Exception ignored) {
            // Not every source tells the size.
        }
        return 0;
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }

    @Override
    public void onDestroy() {
        running = false;
        if (daemon != null) daemon.destroy();
        if (multicast != null && multicast.isHeld()) multicast.release();
        try {
            getSystemService(ConnectivityManager.class).unregisterNetworkCallback(networks);
        } catch (Exception ignored) {
            // It was never registered: the daemon did not start.
        }
        url = null;
        super.onDestroy();
    }

    private PendingIntent openApp() {
        return PendingIntent.getActivity(this, 0, new Intent(this, MainActivity.class), PendingIntent.FLAG_IMMUTABLE);
    }

    /** Received files go to the shared Download folder when the user allowed it. */
    private File downloads() {
        if (Build.VERSION.SDK_INT >= 30 && Environment.isExternalStorageManager()) {
            return Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS);
        }
        return getExternalFilesDir(Environment.DIRECTORY_DOWNLOADS);
    }

    private void runDaemon() {
        try {
            File program = new File(getApplicationInfo().nativeLibraryDir, "libperekat.so");
            ProcessBuilder builder = new ProcessBuilder(
                    program.getAbsolutePath(), "phone",
                    "-dir", getFilesDir().getAbsolutePath(),
                    "-downloads", downloads().getAbsolutePath(),
                    "-name", Build.MODEL,
                    "-nets", String.join(",", addresses()));
            builder.environment().put("HOME", Environment.getExternalStorageDirectory().getAbsolutePath());
            builder.environment().put("TMPDIR", getCacheDir().getAbsolutePath());
            daemon = builder.start();

            new Thread(() -> copyToLog(daemon.getErrorStream()), "daemon-log").start();
            BufferedReader out = new BufferedReader(new InputStreamReader(daemon.getInputStream(), StandardCharsets.UTF_8));
            String address = out.readLine();
            if (address == null || !address.contains("/#")) {
                error = "the daemon did not start";
                return;
            }
            base = address.substring(0, address.indexOf('#'));
            token = address.substring(address.indexOf('#') + 1);
            url = address;

            new Thread(this::serveDaemon, "host").start();
            new Thread(this::reportBattery, "battery").start();
            new Thread(this::publishDevices, "shortcuts").start();
            getSystemService(ConnectivityManager.class).registerNetworkCallback(
                    new NetworkRequest.Builder().build(), networks);
            int code = daemon.waitFor();
            if (running) error = "the daemon stopped (code " + code + ")";
        } catch (Exception e) {
            Log.e(TAG, "daemon", e);
            error = String.valueOf(e.getMessage());
        }
    }

    /**
     * The phone's IPv4 addresses as "address/prefix length". The daemon cannot
     * list them itself: Android keeps that from programs. Networks without
     * broadcast, such as a VPN, are given a prefix of 32.
     */
    private static List<String> addresses() {
        List<String> list = new ArrayList<>();
        try {
            for (NetworkInterface network : Collections.list(NetworkInterface.getNetworkInterfaces())) {
                if (!network.isUp() || network.isLoopback()) continue;
                for (InterfaceAddress address : network.getInterfaceAddresses()) {
                    if (!(address.getAddress() instanceof Inet4Address)) continue;
                    int prefix = address.getBroadcast() == null ? 32 : address.getNetworkPrefixLength();
                    list.add(address.getAddress().getHostAddress() + "/" + prefix);
                }
            }
        } catch (Exception e) {
            Log.w(TAG, "addresses", e);
        }
        return list;
    }

    /** Tells the daemon whenever the phone joins or leaves a network. */
    private final ConnectivityManager.NetworkCallback networks = new ConnectivityManager.NetworkCallback() {
        @Override
        public void onAvailable(Network network) {
            report();
        }

        @Override
        public void onLost(Network network) {
            report();
        }

        @Override
        public void onLinkPropertiesChanged(Network network, LinkProperties properties) {
            report();
        }

        private void report() {
            // Addresses settle a moment after the network changes.
            main.postDelayed(() -> new Thread(() -> {
                try {
                    call("POST", "host/network", new JSONArray(addresses()).toString(), 5000);
                } catch (Exception ignored) {
                    // The daemon is not up; it gets the addresses when it starts.
                }
            }).start(), 500);
        }
    };

    private void copyToLog(InputStream stream) {
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(stream, StandardCharsets.UTF_8))) {
            for (String line; (line = reader.readLine()) != null; ) Log.i(TAG, line);
        } catch (Exception ignored) {
            // The daemon is gone.
        }
    }

    /** Asks the daemon, over and over, what it needs done. */
    private void serveDaemon() {
        while (running) {
            try {
                String answer = call("GET", "host/next", null, 40_000);
                if (answer.isEmpty()) continue;
                JSONObject event = new JSONObject(answer);
                switch (event.optString("type")) {
                    case "clipboard":
                        String text = event.optString("text");
                        main.post(() -> getSystemService(ClipboardManager.class)
                                .setPrimaryClip(ClipData.newPlainText("Перекат", text)));
                        break;
                    case "notify":
                        getSystemService(NotificationManager.class).notify(nextNotification++,
                                new Notification.Builder(this, CHANNEL_EVENTS)
                                        .setSmallIcon(android.R.drawable.stat_notify_sync_noanim)
                                        .setContentTitle(event.optString("title"))
                                        .setContentText(event.optString("body"))
                                        .setContentIntent(openApp())
                                        .setAutoCancel(true)
                                        .build());
                        break;
                    case "ring":
                        main.post(this::ring);
                        break;
                }
            } catch (Exception e) {
                sleep(1000);
            }
        }
    }

    private void ring() {
        Ringtone ringtone = RingtoneManager.getRingtone(this, RingtoneManager.getDefaultUri(RingtoneManager.TYPE_RINGTONE));
        if (ringtone == null) return;
        ringtone.play();
        main.postDelayed(ringtone::stop, 6000);
    }

    private void reportBattery() {
        while (running) {
            try {
                Intent state = registerReceiver(null, new IntentFilter(Intent.ACTION_BATTERY_CHANGED));
                if (state != null) {
                    int level = state.getIntExtra(BatteryManager.EXTRA_LEVEL, -1);
                    int scale = state.getIntExtra(BatteryManager.EXTRA_SCALE, 100);
                    int status = state.getIntExtra(BatteryManager.EXTRA_STATUS, -1);
                    boolean charging = status == BatteryManager.BATTERY_STATUS_CHARGING
                            || status == BatteryManager.BATTERY_STATUS_FULL;
                    JSONObject body = new JSONObject();
                    body.put("charge", level < 0 ? -1 : level * 100 / scale);
                    body.put("charging", charging);
                    call("POST", "host/battery", body.toString(), 5000);
                }
            } catch (Exception ignored) {
                // Tried again in a minute.
            }
            sleep(60_000);
        }
    }

    /**
     * Keeps the paired devices published as sharing shortcuts, so the system
     * offers them by name in its share sheet.
     */
    private void publishDevices() {
        String published = null;
        while (running) {
            try {
                JSONArray devices = new JSONArray(call("GET", "devices", null, 5000));
                List<ShortcutInfo> shortcuts = new ArrayList<>();
                StringBuilder now = new StringBuilder();
                for (int i = 0; i < devices.length() && shortcuts.size() < 4; i++) {
                    JSONObject d = devices.getJSONObject(i);
                    if (!d.optBoolean("paired") || d.optBoolean("foreign")) continue;
                    String id = d.getString("id");
                    String name = d.getString("name");
                    now.append(id).append('=').append(name).append(';');
                    shortcuts.add(new ShortcutInfo.Builder(this, ShareActivity.SHORTCUT_PREFIX + id)
                            .setShortLabel(name)
                            .setIcon(Icon.createWithResource(this,
                                    "phone".equals(d.optString("type")) ? R.drawable.ic_phone : R.drawable.ic_computer))
                            .setIntent(new Intent(this, ShareActivity.class).setAction(Intent.ACTION_SEND))
                            .setCategories(Collections.singleton("io.github.charg3d.perekat.category.DEVICE"))
                            .setLongLived(true)
                            .setRank(shortcuts.size())
                            .build());
                }
                if (!now.toString().equals(published)) {
                    getSystemService(ShortcutManager.class).setDynamicShortcuts(shortcuts);
                    published = now.toString();
                }
            } catch (Exception e) {
                Log.w(TAG, "shortcuts", e);
            }
            sleep(20_000);
        }
    }

    /** Tells the daemon what the user copied. */
    static void clipboardChanged(String text) {
        if (base == null || text == null || text.isEmpty()) return;
        new Thread(() -> {
            try {
                call("POST", "host/clipboard", JSONObject.quote(text), 5000);
            } catch (Exception ignored) {
                // The daemon is not up; the next copy will get through.
            }
        }).start();
    }

    /** Waits for the daemon to come up; false if it does not in time. */
    static boolean waitReady(long millis) {
        for (long waited = 0; base == null && error == null && waited < millis; waited += 100) sleep(100);
        return base != null;
    }

    /** Starts a call to the daemon's local API. */
    static HttpURLConnection open(String method, String path) throws Exception {
        HttpURLConnection connection = (HttpURLConnection) new URL(base + "api/" + path).openConnection();
        connection.setRequestMethod(method);
        connection.setRequestProperty("X-Perekat-Token", token);
        connection.setConnectTimeout(5000);
        return connection;
    }

    static String read(InputStream stream) throws Exception {
        StringBuilder text = new StringBuilder();
        if (stream == null) return "";
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(stream, StandardCharsets.UTF_8))) {
            for (String line; (line = reader.readLine()) != null; ) text.append(line);
        }
        return text.toString();
    }

    static String call(String method, String path, String body, int timeout) throws Exception {
        HttpURLConnection connection = open(method, path);
        try {
            connection.setReadTimeout(timeout);
            if (body != null) {
                connection.setDoOutput(true);
                try (OutputStream out = connection.getOutputStream()) {
                    out.write(body.getBytes(StandardCharsets.UTF_8));
                }
            }
            if (connection.getResponseCode() != 200) throw new Exception(read(connection.getErrorStream()));
            return read(connection.getInputStream());
        } finally {
            connection.disconnect();
        }
    }

    private static void sleep(long millis) {
        try {
            Thread.sleep(millis);
        } catch (InterruptedException ignored) {
            Thread.currentThread().interrupt();
        }
    }
}
