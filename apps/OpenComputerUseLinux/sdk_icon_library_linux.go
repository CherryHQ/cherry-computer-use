package main

import (
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
)

func loadIconLibrary() func(string) []byte {
	library, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil
	}
	var init func(uintptr, uintptr) int32
	var defaultTheme func() uintptr
	var newTheme func() uintptr
	var setTheme func(uintptr, string)
	var load func(uintptr, string, int32, int32, uintptr) uintptr
	var fromFile func(string, int32, int32, int32, uintptr) uintptr
	var scale func(uintptr, int32, int32, int32) uintptr
	var save func(uintptr, *unsafe.Pointer, *uintptr, string, uintptr, uintptr, uintptr) int32
	var unref func(uintptr)
	var free func(unsafe.Pointer)
	purego.RegisterLibFunc(&init, library, "gtk_init_check")
	purego.RegisterLibFunc(&defaultTheme, library, "gtk_icon_theme_get_default")
	purego.RegisterLibFunc(&newTheme, library, "gtk_icon_theme_new")
	purego.RegisterLibFunc(&setTheme, library, "gtk_icon_theme_set_custom_theme")
	purego.RegisterLibFunc(&load, library, "gtk_icon_theme_load_icon")
	purego.RegisterLibFunc(&fromFile, library, "gdk_pixbuf_new_from_file_at_scale")
	purego.RegisterLibFunc(&scale, library, "gdk_pixbuf_scale_simple")
	purego.RegisterLibFunc(&save, library, "gdk_pixbuf_save_to_bufferv")
	purego.RegisterLibFunc(&unref, library, "g_object_unref")
	purego.RegisterLibFunc(&free, library, "g_free")
	var theme uintptr
	if init(0, 0) != 0 {
		theme = defaultTheme()
	} else {
		theme = newTheme()
		setTheme(theme, "hicolor")
	}
	return func(name string) []byte {
		var pixbuf uintptr
		if filepath.IsAbs(name) {
			pixbuf = fromFile(name, 64, 64, 0, 0)
		} else if theme != 0 && !strings.ContainsRune(name, '/') {
			pixbuf = load(theme, name, 64, 16, 0)
		}
		if pixbuf == 0 {
			return nil
		}
		defer unref(pixbuf)
		sized := scale(pixbuf, 64, 64, 2)
		if sized == 0 {
			return nil
		}
		defer unref(sized)
		var buffer unsafe.Pointer
		var length uintptr
		if save(sized, &buffer, &length, "png", 0, 0, 0) == 0 {
			return nil
		}
		defer free(buffer)
		if length == 0 || length > 49152 {
			return nil
		}
		return append([]byte(nil), unsafe.Slice((*byte)(buffer), int(length))...)
	}
}
