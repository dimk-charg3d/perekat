package io.github.charg3d.perekat;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.Intent;
import android.database.Cursor;
import android.net.Uri;
import android.os.Bundle;
import android.provider.OpenableColumns;
import android.widget.Toast;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

/**
 * What "Share" in another application leads to: files, text and links go to a
 * paired device or into a network folder. It has no screen of its own, only a
 * list to choose from and a note while the files are on their way.
 */
public class ShareActivity extends Activity {
    static final String ACTION_PICK = "io.github.charg3d.perekat.PICK";
    static final String SHORTCUT_PREFIX = "device:";
    private static final int PICK_FILES = 1;

    /** A place to send to: a device, or a network folder on one. */
    private static class Target {
        String device;
        String label;
        String folder;
    }

    private final List<Uri> files = new ArrayList<>();
    private String text;
    private AlertDialog dialog;

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        startForegroundService(new Intent(this, CoreService.class));
        Intent intent = getIntent();
        if (ACTION_PICK.equals(intent.getAction())) {
            Intent pick = new Intent(Intent.ACTION_GET_CONTENT);
            pick.addCategory(Intent.CATEGORY_OPENABLE);
            pick.setType("*/*");
            pick.putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true);
            startActivityForResult(pick, PICK_FILES);
            return;
        }
        collect(intent);
        begin();
    }

    @Override
    protected void onActivityResult(int request, int result, Intent data) {
        if (request != PICK_FILES || result != RESULT_OK || data == null) {
            finish();
            return;
        }
        collect(data);
        begin();
    }

    private void collect(Intent intent) {
        Uri one = intent.getParcelableExtra(Intent.EXTRA_STREAM);
        List<Uri> many = intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM);
        if (many != null) files.addAll(many);
        else if (one != null) files.add(one);
        ClipData clip = intent.getClipData();
        if (files.isEmpty() && clip != null) {
            for (int i = 0; i < clip.getItemCount(); i++) {
                if (clip.getItemAt(i).getUri() != null) files.add(clip.getItemAt(i).getUri());
            }
        }
        if (files.isEmpty() && intent.getData() != null) files.add(intent.getData());
        CharSequence shared = intent.getCharSequenceExtra(Intent.EXTRA_TEXT);
        if (files.isEmpty() && shared != null) text = shared.toString();
    }

    private void begin() {
        if (files.isEmpty() && (text == null || text.trim().isEmpty())) {
            done(getString(R.string.share_nothing));
            return;
        }
        new Thread(() -> {
            List<Target> targets = new ArrayList<>();
            try {
                if (!CoreService.waitReady(8000)) throw new Exception(getString(R.string.not_ready));
                JSONArray devices = new JSONArray(CoreService.call("GET", "devices", null, 5000));
                for (int i = 0; i < devices.length(); i++) {
                    JSONObject d = devices.getJSONObject(i);
                    if (!d.optBoolean("paired") || !d.optBoolean("connected")) continue;
                    Target device = new Target();
                    device.device = d.getString("id");
                    device.label = d.getString("name");
                    targets.add(device);
                    JSONArray shares = d.optJSONArray("shares");
                    // Only files have a place in a folder.
                    for (int j = 0; shares != null && !files.isEmpty() && j < shares.length(); j++) {
                        Target folder = new Target();
                        folder.device = device.device;
                        folder.label = shares.getJSONObject(j).getString("name") + " · " + device.label;
                        folder.folder = shares.getJSONObject(j).getString("path");
                        targets.add(folder);
                    }
                }
            } catch (Exception e) {
                runOnUiThread(() -> done(String.valueOf(e.getMessage())));
                return;
            }
            runOnUiThread(() -> choose(targets));
        }).start();
    }

    private void choose(List<Target> targets) {
        if (isFinishing()) return;
        if (targets.isEmpty()) {
            done(getString(R.string.share_none));
            return;
        }
        // A device picked right in the share sheet needs no second question.
        String shortcut = getIntent().getStringExtra(Intent.EXTRA_SHORTCUT_ID);
        for (Target target : targets) {
            if (target.folder == null && (SHORTCUT_PREFIX + target.device).equals(shortcut)) {
                send(target);
                return;
            }
        }
        if (targets.size() == 1) {
            send(targets.get(0));
            return;
        }
        String[] labels = new String[targets.size()];
        for (int i = 0; i < labels.length; i++) labels[i] = targets.get(i).label;
        dialog = new AlertDialog.Builder(this, android.R.style.Theme_DeviceDefault_Dialog_Alert)
                .setTitle(R.string.share_title)
                .setItems(labels, (d, which) -> send(targets.get(which)))
                .setOnCancelListener(d -> finish())
                .show();
    }

    private void send(Target target) {
        dialog = new AlertDialog.Builder(this, android.R.style.Theme_DeviceDefault_Dialog_Alert)
                .setMessage(getString(R.string.share_sending, target.label))
                .setCancelable(false)
                .show();
        new Thread(() -> {
            String result;
            try {
                String device = "?device=" + URLEncoder.encode(target.device, "UTF-8");
                if (files.isEmpty()) {
                    CoreService.call("POST", "text" + device, JSONObject.quote(text), 15000);
                } else if (target.folder != null) {
                    upload("fs/upload" + device + "&dir=" + URLEncoder.encode(target.folder, "UTF-8"));
                } else {
                    upload("upload" + device);
                }
                result = getString(R.string.share_done, target.label);
            } catch (Exception e) {
                result = getString(R.string.share_failed, String.valueOf(e.getMessage()));
            }
            String message = result;
            runOnUiThread(() -> done(message));
        }).start();
    }

    /** Streams the shared files to the daemon, which passes them on. */
    private void upload(String path) throws Exception {
        String boundary = "perekat" + System.nanoTime();
        HttpURLConnection connection = CoreService.open("POST", path);
        try {
            connection.setReadTimeout(0);
            connection.setDoOutput(true);
            connection.setChunkedStreamingMode(64 * 1024);
            connection.setRequestProperty("Content-Type", "multipart/form-data; boundary=" + boundary);
            try (OutputStream out = connection.getOutputStream()) {
                byte[] buffer = new byte[64 * 1024];
                for (Uri file : files) {
                    String name = nameOf(file).replace("\"", "'").replace("\r", "").replace("\n", "");
                    out.write(("--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"" + name
                            + "\"\r\nContent-Type: application/octet-stream\r\n\r\n").getBytes(StandardCharsets.UTF_8));
                    try (InputStream in = getContentResolver().openInputStream(file)) {
                        if (in == null) throw new Exception(name);
                        for (int n; (n = in.read(buffer)) > 0; ) out.write(buffer, 0, n);
                    }
                    out.write("\r\n".getBytes(StandardCharsets.UTF_8));
                }
                out.write(("--" + boundary + "--\r\n").getBytes(StandardCharsets.UTF_8));
            }
            if (connection.getResponseCode() != 200) throw new Exception(CoreService.read(connection.getErrorStream()));
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

    private void done(String message) {
        if (dialog != null && dialog.isShowing()) dialog.dismiss();
        Toast.makeText(getApplicationContext(), message, Toast.LENGTH_LONG).show();
        finish();
    }

    @Override
    protected void onDestroy() {
        if (dialog != null && dialog.isShowing()) dialog.dismiss();
        super.onDestroy();
    }
}
