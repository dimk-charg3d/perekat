package io.github.charg3d.perekat;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;

/** Brings perekat up when the phone has started, so nobody has to open it. */
public class BootReceiver extends BroadcastReceiver {
    @Override
    public void onReceive(Context context, Intent intent) {
        if (Intent.ACTION_BOOT_COMPLETED.equals(intent.getAction())) {
            context.startForegroundService(new Intent(context, CoreService.class));
        }
    }
}
