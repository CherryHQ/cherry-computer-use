#include <gtk/gtk.h>
#include <gdk/gdkx.h>
#include <stdio.h>

static void increment(GtkButton *button, gpointer data) {
    static int count = 0;
    char label[32];
    snprintf(label, sizeof(label), "Count: %d", ++count);
    gtk_button_set_label(button, label);
}

int main(int argc, char **argv) {
    gtk_init(&argc, &argv);
    GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
    gtk_window_set_title(GTK_WINDOW(window), "Cherry Native Probe");
    gtk_window_set_default_size(GTK_WINDOW(window), 320, 160);
    GtkWidget *button = gtk_button_new_with_label("Count: 0");
    g_signal_connect(button, "clicked", G_CALLBACK(increment), NULL);
    gtk_container_add(GTK_CONTAINER(window), button);
    gtk_widget_show_all(window);
    printf("%lu\n", gdk_x11_window_get_xid(gtk_widget_get_window(window)));
    fflush(stdout);
    gtk_main();
    return 0;
}
