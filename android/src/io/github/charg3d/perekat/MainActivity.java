package io.github.charg3d.perekat;

import android.Manifest;
import android.app.Activity;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Environment;
import android.os.Handler;
import android.os.Looper;
import android.provider.Settings;
import android.webkit.JavascriptInterface;
import android.webkit.ValueCallback;
import android.webkit.WebChromeClient;
import android.webkit.WebView;
import android.webkit.WebViewClient;

/**
 * The screen: a web view showing the same interface as the desktop window,
 * served by the daemon that CoreService runs.
 */
public class MainActivity extends Activity {
    private static final int PICK_FILES = 1;

    private WebView web;
    private ValueCallback<Uri[]> picker;
    private final Handler main = new Handler(Looper.getMainLooper());
    private final ClipboardManager.OnPrimaryClipChangedListener copied = this::shareClipboard;

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        startForegroundService(new Intent(this, CoreService.class));
        askPermissions();

        web = new WebView(this);
        web.setBackgroundColor(Color.rgb(0x16, 0x18, 0x1a));
        web.getSettings().setJavaScriptEnabled(true);
        web.getSettings().setDomStorageEnabled(true);
        web.setWebViewClient(new WebViewClient());
        web.setWebChromeClient(new WebChromeClient() {
            @Override
            public boolean onShowFileChooser(WebView view, ValueCallback<Uri[]> callback, FileChooserParams params) {
                if (picker != null) picker.onReceiveValue(null);
                picker = callback;
                Intent pick = new Intent(Intent.ACTION_GET_CONTENT);
                pick.addCategory(Intent.CATEGORY_OPENABLE);
                pick.setType("*/*");
                pick.putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true);
                startActivityForResult(pick, PICK_FILES);
                return true;
            }
        });
        web.addJavascriptInterface(new Bridge(), "Android");
        setContentView(web);
        openWhenReady();
        joinFromLink(getIntent());

        getSystemService(ClipboardManager.class).addPrimaryClipChangedListener(copied);
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        joinFromLink(intent);
        if ("files".equals(intent.getStringExtra("view"))) {
            web.evaluateJavascript("show({ kind: 'files', id: 'self', path: '' })", null);
        }
    }

    /** A perekat:// link, read from a QR code, connects this phone to remote access. */
    private void joinFromLink(Intent intent) {
        Uri link = intent.getData();
        if (link == null || !"perekat".equals(link.getScheme())) return;
        String code = link.toString();
        new Thread(() -> {
            String result = getString(R.string.joined);
            try {
                if (!CoreService.waitReady(8000)) throw new Exception(getString(R.string.not_ready));
                CoreService.call("POST", "relay/join", org.json.JSONObject.quote(code), 20000);
            } catch (Exception e) {
                result = String.valueOf(e.getMessage());
            }
            String message = result;
            runOnUiThread(() -> android.widget.Toast.makeText(this, message, android.widget.Toast.LENGTH_LONG).show());
        }).start();
    }

    /** Loads the interface as soon as the daemon reports where it is. */
    private void openWhenReady() {
        String url = CoreService.url;
        if (url != null) {
            // A shortcut may ask for a page other than the first one.
            String page = getIntent().getStringExtra("view");
            web.loadUrl(page == null ? url : url.replace("/#", "/?view=" + page + "#"));
        } else if (CoreService.error != null) {
            web.loadDataWithBaseURL(null, "<body style='background:#16181a;color:#e5484d;font:16px sans-serif;padding:24px'>"
                    + CoreService.error + "</body>", "text/html", "utf-8", null);
        } else {
            main.postDelayed(this::openWhenReady, 150);
        }
    }

    private void askPermissions() {
        if (Build.VERSION.SDK_INT >= 33
                && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, 0);
        }
        // Access to all files is asked for once; the user may well say no.
        SharedPreferences prefs = getPreferences(MODE_PRIVATE);
        if (Build.VERSION.SDK_INT >= 30 && !Environment.isExternalStorageManager() && !prefs.getBoolean("askedFiles", false)) {
            prefs.edit().putBoolean("askedFiles", true).apply();
            try {
                startActivity(new Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION,
                        Uri.parse("package:" + getPackageName())));
            } catch (Exception e) {
                startActivity(new Intent(Settings.ACTION_MANAGE_ALL_FILES_ACCESS_PERMISSION));
            }
        }
    }

    /** Android shows the clipboard only to the application on screen. */
    private void shareClipboard() {
        if (!hasWindowFocus()) return;
        ClipData clip = getSystemService(ClipboardManager.class).getPrimaryClip();
        if (clip == null || clip.getItemCount() == 0) return;
        CharSequence text = clip.getItemAt(0).coerceToText(this);
        if (text != null) CoreService.clipboardChanged(text.toString());
    }

    @Override
    public void onWindowFocusChanged(boolean focused) {
        super.onWindowFocusChanged(focused);
        if (focused) shareClipboard();
    }

    @Override
    protected void onActivityResult(int request, int result, Intent data) {
        if (request != PICK_FILES || picker == null) return;
        Uri[] files = null;
        if (result == RESULT_OK && data != null) {
            ClipData many = data.getClipData();
            if (many != null) {
                files = new Uri[many.getItemCount()];
                for (int i = 0; i < files.length; i++) files[i] = many.getItemAt(i).getUri();
            } else if (data.getData() != null) {
                files = new Uri[]{data.getData()};
            }
        }
        picker.onReceiveValue(files);
        picker = null;
    }

    /** Back goes up within the interface first, and leaves it running after that. */
    @Override
    public void onBackPressed() {
        web.evaluateJavascript("window.perekatBack ? window.perekatBack() : false", handled -> {
            if (!"true".equals(handled)) moveTaskToBack(true);
        });
    }

    @Override
    protected void onDestroy() {
        getSystemService(ClipboardManager.class).removePrimaryClipChangedListener(copied);
        super.onDestroy();
    }

    private class Bridge {
        /** Opens the system screen where access to notifications is given. */
        @JavascriptInterface
        public void notificationAccess() {
            main.post(() -> startActivity(new Intent(Settings.ACTION_NOTIFICATION_LISTENER_SETTINGS)));
        }

        /** Asks for the right to lock the screen on a computer's request. */
        @JavascriptInterface
        public void lockAccess() {
            main.post(() -> startActivity(new Intent(android.app.admin.DevicePolicyManager.ACTION_ADD_DEVICE_ADMIN)
                    .putExtra(android.app.admin.DevicePolicyManager.EXTRA_DEVICE_ADMIN,
                            new android.content.ComponentName(MainActivity.this, LockAdmin.class))
                    .putExtra(android.app.admin.DevicePolicyManager.EXTRA_ADD_EXPLANATION, getString(R.string.lock_admin))));
        }

        @JavascriptInterface
        public void quit() {
            main.post(() -> {
                stopService(new Intent(MainActivity.this, CoreService.class));
                finishAndRemoveTask();
            });
        }
    }
}
