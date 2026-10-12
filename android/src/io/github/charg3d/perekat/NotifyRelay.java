package io.github.charg3d.perekat;

import android.app.Notification;
import android.content.pm.ApplicationInfo;
import android.service.notification.NotificationListenerService;
import android.service.notification.StatusBarNotification;

import org.json.JSONObject;

/**
 * Passes the phone's notifications on to the paired computers. It works only
 * after the user has allowed it in the system settings.
 */
public class NotifyRelay extends NotificationListenerService {
    private String last = "";

    @Override
    public void onNotificationPosted(StatusBarNotification posted) {
        Notification n = posted.getNotification();
        if (n == null || posted.getPackageName().equals(getPackageName())) return;
        // Progress bars, players and the summaries of groups are not news.
        if (posted.isOngoing() || (n.flags & Notification.FLAG_GROUP_SUMMARY) != 0) return;
        CharSequence title = n.extras.getCharSequence(Notification.EXTRA_TITLE);
        CharSequence text = n.extras.getCharSequence(Notification.EXTRA_TEXT);
        if (title == null && text == null) return;
        String key = posted.getKey() + "|" + title + "|" + text;
        if (key.equals(last)) return;
        last = key;
        String app = posted.getPackageName();
        try {
            ApplicationInfo info = getPackageManager().getApplicationInfo(app, 0);
            app = String.valueOf(getPackageManager().getApplicationLabel(info));
        } catch (Exception ignored) {
            // The package name will do.
        }
        try {
            JSONObject body = new JSONObject();
            body.put("app", app);
            body.put("title", title == null ? "" : title.toString());
            body.put("text", text == null ? "" : text.toString());
            String json = body.toString();
            new Thread(() -> {
                try {
                    if (CoreService.waitReady(0)) CoreService.call("POST", "host/notification", json, 5000);
                } catch (Exception ignored) {
                    // The daemon is not up; this one is missed.
                }
            }).start();
        } catch (Exception ignored) {
            // Nothing to send.
        }
    }
}
