package com.example.vpn_routing;

import android.content.Context;
import android.graphics.drawable.ColorDrawable;
import android.graphics.drawable.Drawable;
import android.graphics.drawable.GradientDrawable;
import android.graphics.drawable.LayerDrawable;
import android.view.Gravity;

import androidx.core.content.ContextCompat;

/** Фон строки в сгруппированном списке: общий блок со скруглёнными краями и разделителями. */
final class GroupBackground {

    private GroupBackground() {
    }

    static Drawable create(Context c, boolean first, boolean last) {
        float d = c.getResources().getDisplayMetrics().density;
        float r = 16 * d;
        GradientDrawable fill = new GradientDrawable();
        fill.setColor(ContextCompat.getColor(c, R.color.surface));
        fill.setCornerRadii(new float[]{
                first ? r : 0, first ? r : 0, first ? r : 0, first ? r : 0,
                last ? r : 0, last ? r : 0, last ? r : 0, last ? r : 0});
        if (last) return fill;
        // Разделитель под строкой, с отступом под значок
        ColorDrawable line = new ColorDrawable(ContextCompat.getColor(c, R.color.outline));
        LayerDrawable layers = new LayerDrawable(new Drawable[]{fill, line});
        layers.setLayerGravity(1, Gravity.BOTTOM | Gravity.FILL_HORIZONTAL);
        layers.setLayerHeight(1, Math.max(1, Math.round(d)));
        layers.setLayerInsetStart(1, Math.round(60 * d));
        return layers;
    }
}
