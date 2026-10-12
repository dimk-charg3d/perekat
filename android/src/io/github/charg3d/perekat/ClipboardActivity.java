package io.github.charg3d.perekat;

import android.app.Activity;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Intent;
import android.os.Bundle;
import android.widget.Toast;

import org.json.JSONObject;

/**
 * Sends the clipboard to the other devices. Android shows the clipboard only
 * to the application on screen, so the tile and the shortcut bring this
 * invisible screen to the front for the moment it takes to read it.
 */
public class ClipboardActivity extends Activity {
    private boolean sent;

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        startForegroundService(new Intent(this, CoreService.class));
    }

    @Override
    public void onWindowFocusChanged(boolean focused) {
        super.onWindowFocusChanged(focused);
        if (!focused || sent) return;
        sent = true;
        ClipData clip = getSystemService(ClipboardManager.class).getPrimaryClip();
        CharSequence text = clip == null || clip.getItemCount() == 0 ? null : clip.getItemAt(0).coerceToText(this);
        if (text == null || text.length() == 0) {
            done(getString(R.string.clip_empty));
            return;
        }
        String body = JSONObject.quote(text.toString());
        new Thread(() -> {
            String message = getString(R.string.clip_sent);
            try {
                if (!CoreService.waitReady(8000)) throw new Exception(getString(R.string.not_ready));
                CoreService.call("POST", "host/clipboard", body, 5000);
            } catch (Exception e) {
                message = String.valueOf(e.getMessage());
            }
            String result = message;
            runOnUiThread(() -> done(result));
        }).start();
    }

    private void done(String message) {
        Toast.makeText(getApplicationContext(), message, Toast.LENGTH_SHORT).show();
        finish();
    }
}
