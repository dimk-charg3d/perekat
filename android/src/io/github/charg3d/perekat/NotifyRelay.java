package io.github.charg3d.perekat;

import android.app.Notification;
import android.app.RemoteInput;
import android.content.Intent;
import android.content.pm.ApplicationInfo;
import android.os.Bundle;
import android.service.notification.NotificationListenerService;
import android.service.notification.StatusBarNotification;

import org.json.JSONObject;

import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Passes the phone's notifications on to the paired computers, tells them of
 * phone calls, and sends the replies typed there. It works only after the
 * user has allowed it in the system settings.
 */
public class NotifyRelay extends NotificationListenerService {
    private static volatile NotifyRelay running;

    private String last = "";
    /** The messages that can still be answered, by notification key. */
    private final Map<String, Notification.Action> replies = new LinkedHashMap<String, Notification.Action>() {
        @Override
        protected boolean removeEldestEntry(Map.Entry<String, Notification.Action> eldest) {
            return size() > 60;
        }
    };

    @Override
    public void onListenerConnected() {
        running = this;
    }

    @Override
    public void onListenerDisconnected() {
        running = null;
    }

    private static boolean isCall(Notification n) {
        return Notification.CATEGORY_CALL.equals(n.category);
    }

    @Override
    public void onNotificationPosted(StatusBarNotification posted) {
        Notification n = posted.getNotification();
        if (n == null || posted.getPackageName().equals(getPackageName())) return;
        boolean call = isCall(n);
        // Progress bars, players and the summaries of groups are not news; a
        // call is, although it stays on the screen like they do.
        if (!call && (posted.isOngoing() || (n.flags & Notification.FLAG_GROUP_SUMMARY) != 0)) return;
        CharSequence title = n.extras.getCharSequence(Notification.EXTRA_TITLE);
        CharSequence text = n.extras.getCharSequence(Notification.EXTRA_TEXT);
        if (title == null && text == null) return;
        String key = posted.getKey() + "|" + title + "|" + text;
        if (key.equals(last)) return;
        last = key;

        Notification.Action reply = null;
        if (n.actions != null) {
            for (Notification.Action action : n.actions) {
                RemoteInput[] inputs = action.getRemoteInputs();
                if (inputs != null && inputs.length > 0) reply = action;
            }
        }
        synchronized (replies) {
            if (reply != null) replies.put(posted.getKey(), reply);
        }

        String app = posted.getPackageName();
        try {
            ApplicationInfo info = getPackageManager().getApplicationInfo(app, 0);
            app = String.valueOf(getPackageManager().getApplicationLabel(info));
        } catch (Exception ignored) {
            // The package name will do.
        }
        try {
            JSONObject body = new JSONObject();
            body.put("key", posted.getKey());
            body.put("app", app);
            body.put("title", title == null ? "" : title.toString());
            body.put("text", text == null ? "" : text.toString());
            body.put("canReply", reply != null);
            body.put("call", call);
            send(body);
        } catch (Exception ignored) {
            // Nothing to send.
        }
    }

    @Override
    public void onNotificationRemoved(StatusBarNotification removed) {
        Notification n = removed.getNotification();
        if (n == null || removed.getPackageName().equals(getPackageName())) return;
        synchronized (replies) {
            replies.remove(removed.getKey());
        }
        try {
            JSONObject body = new JSONObject();
            body.put("key", removed.getKey());
            body.put("end", true);
            body.put("call", isCall(n));
            send(body);
        } catch (Exception ignored) {
            // Nothing to send.
        }
    }

    private static void send(JSONObject body) {
        String json = body.toString();
        new Thread(() -> {
            try {
                if (CoreService.waitReady(0)) CoreService.call("POST", "host/notification", json, 5000);
            } catch (Exception ignored) {
                // The daemon is not up; this one is missed.
            }
        }).start();
    }

    /** Answers a message with text typed on a computer. */
    static boolean reply(String key, String text) {
        NotifyRelay relay = running;
        if (relay == null) return false;
        Notification.Action action;
        synchronized (relay.replies) {
            action = relay.replies.get(key);
        }
        if (action == null) return false;
        try {
            Intent filled = new Intent();
            Bundle results = new Bundle();
            for (RemoteInput input : action.getRemoteInputs()) results.putCharSequence(input.getResultKey(), text);
            RemoteInput.addResultsToIntent(action.getRemoteInputs(), filled, results);
            action.actionIntent.send(relay, 0, filled);
            return true;
        } catch (Exception e) {
            return false;
        }
    }
}
