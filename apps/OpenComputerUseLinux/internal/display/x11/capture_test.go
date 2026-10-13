package x11

import (
	"testing"

	"github.com/jezek/xgb/xproto"
)

func TestCaptureDecodesDepth32Windows(t *testing.T) {
	cases := []struct {
		name   string
		order  byte
		depth  byte
		width  uint16
		height uint16
		data   []byte
		want   bool
	}{
		{"plain depth 24 window", xproto.ImageOrderLSBFirst, 24, 320, 160, make([]byte, 320*160*4), true},
		{"ARGB depth 32 window", xproto.ImageOrderLSBFirst, 32, 320, 160, make([]byte, 320*160*4), true},
		{"depth 16 window", xproto.ImageOrderLSBFirst, 16, 320, 160, make([]byte, 320*160*2), false},
		{"big endian server", xproto.ImageOrderMSBFirst, 24, 320, 160, make([]byte, 320*160*4), false},
		{"short reply", xproto.ImageOrderLSBFirst, 24, 320, 160, make([]byte, 320*160*4-4), false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := decodablePixelFormat(testCase.order, testCase.depth, testCase.width, testCase.height, testCase.data); got != testCase.want {
				t.Fatalf("decodablePixelFormat(depth=%d, order=%v) = %v, want %v", testCase.depth, testCase.order, got, testCase.want)
			}
		})
	}
}
