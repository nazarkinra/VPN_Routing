package com.example.vpn_routing;

import android.os.Bundle;
import android.text.InputType;
import android.view.LayoutInflater;
import android.view.MenuItem;
import android.view.View;
import android.view.ViewGroup;
import android.widget.FrameLayout;
import android.widget.Toast;

import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import com.google.android.material.dialog.MaterialAlertDialogBuilder;
import androidx.appcompat.widget.SearchView;
import androidx.fragment.app.Fragment;
import androidx.recyclerview.widget.LinearLayoutManager;
import androidx.recyclerview.widget.RecyclerView;

import com.google.android.material.appbar.MaterialToolbar;
import com.google.android.material.textfield.TextInputEditText;
import com.google.android.material.textfield.TextInputLayout;

/** Вкладка «Напрямую»: что идёт без VPN, добавление и удаление своих сайтов. */
public class DirectFragment extends Fragment implements DirectAdapter.Callbacks {

    private Settings settings;
    private DirectAdapter adapter;
    private String query = "";

    @Nullable
    @Override
    public View onCreateView(@NonNull LayoutInflater inflater, @Nullable ViewGroup container,
                             @Nullable Bundle savedInstanceState) {
        View v = inflater.inflate(R.layout.fragment_direct, container, false);
        settings = new Settings(requireContext());

        MaterialToolbar toolbar = v.findViewById(R.id.toolbar);
        toolbar.inflateMenu(R.menu.direct);
        MenuItem search = toolbar.getMenu().findItem(R.id.action_search);
        SearchView searchView = (SearchView) search.getActionView();
        searchView.setQueryHint("Домен или сайт");
        searchView.setOnQueryTextListener(new SearchView.OnQueryTextListener() {
            @Override
            public boolean onQueryTextSubmit(String q) {
                return true;
            }

            @Override
            public boolean onQueryTextChange(String q) {
                query = q;
                render();
                return true;
            }
        });

        RecyclerView list = v.findViewById(R.id.list);
        list.setLayoutManager(new LinearLayoutManager(requireContext()));
        adapter = new DirectAdapter(this);
        list.setAdapter(adapter);
        v.findViewById(R.id.addButton).setOnClickListener(x -> showAdd());
        render();
        return v;
    }

    @Override
    public void onHiddenChanged(boolean hidden) {
        super.onHiddenChanged(hidden);
        if (!hidden) render(); // VPN МГТУ могли включить на соседней вкладке
    }

    private void render() {
        // Свои исключения, уже покрытые встроенными правилами (ya.ru при .ru), убираются из списка
        if (settings.compactExceptions() > 0) reloadVpn();
        adapter.submit(settings.getExceptions(), settings.isBmstuEnabled(), query);
    }

    private void showAdd() {
        View frame = getLayoutInflater().inflate(R.layout.dialog_input, null, false);
        TextInputLayout layout = frame.findViewById(R.id.inputLayout);
        TextInputEditText input = frame.findViewById(R.id.input);
        layout.setHint(getString(R.string.add_site_hint));
        input.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        input.setSingleLine(true);
        new MaterialAlertDialogBuilder(requireContext()).setTitle(R.string.add_site)
                .setMessage("Сайт и все его поддомены будут открываться без VPN. Можно вставить адрес страницы целиком или IP-подсеть.")
                .setView(frame)
                .setPositiveButton(R.string.add, (d, w) ->
                        add(input.getText() == null ? "" : input.getText().toString()))
                .setNegativeButton(android.R.string.cancel, null).show();
    }

    private void add(String raw) {
        if (raw.trim().isEmpty()) return;
        String value;
        try {
            value = settings.addException(raw);
        } catch (IllegalArgumentException e) {
            new MaterialAlertDialogBuilder(requireContext()).setTitle("Не удалось добавить").setMessage(e.getMessage())
                    .setPositiveButton(android.R.string.ok, null).show();
            return;
        }
        if (value == null) {
            toast("Этот адрес уже идёт напрямую.");
            return;
        }
        render();
        reloadVpn();
        toast(DirectAdapter.display(value) + " теперь открывается без VPN");
    }

    @Override
    public void onRemove(String value) {
        new MaterialAlertDialogBuilder(requireContext())
                .setMessage("Убрать " + DirectAdapter.display(value) + " из списка? Сайт снова пойдёт через VPN.")
                .setPositiveButton(R.string.remove, (d, w) -> {
                    if (settings.removeException(value)) {
                        render();
                        reloadVpn();
                    }
                })
                .setNegativeButton(android.R.string.cancel, null).show();
    }

    private void reloadVpn() {
        if (RoutingVpnService.getState() == RoutingVpnService.State.CONNECTED) {
            RoutingVpnService.send(requireContext(), RoutingVpnService.ACTION_RELOAD);
        }
    }

    private void toast(String text) {
        if (isAdded()) Toast.makeText(requireContext(), text, Toast.LENGTH_SHORT).show();
    }
}
