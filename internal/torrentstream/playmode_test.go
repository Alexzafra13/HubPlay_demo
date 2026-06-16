package torrentstream

import "testing"

func TestDecidePlayMode(t *testing.T) {
	cases := []struct {
		name string
		info MediaInfo
		want PlayMode
	}{
		{
			name: "mp4 h264 aac → direct",
			info: MediaInfo{Container: "mov,mp4,m4a,3gp,3g2,mj2", VideoCodec: "h264", AudioCodec: "aac"},
			want: PlayDirect,
		},
		{
			name: "webm vp9 opus → direct",
			info: MediaInfo{Container: "matroska,webm", VideoCodec: "vp9", AudioCodec: "opus"},
			want: PlayDirect,
		},
		{
			name: "mkv h264 aac → remux (codecs ok, container not)",
			info: MediaInfo{Container: "matroska,webm", VideoCodec: "h264", AudioCodec: "aac"},
			want: PlayRemux,
		},
		{
			name: "mkv h264 ac3 → remux (audio fixed up)",
			info: MediaInfo{Container: "matroska,webm", VideoCodec: "h264", AudioCodec: "ac3"},
			want: PlayRemux,
		},
		{
			name: "mp4 h264 ac3 → remux (ac3 not browser-playable in mp4)",
			info: MediaInfo{Container: "mov,mp4,m4a,3gp,3g2,mj2", VideoCodec: "h264", AudioCodec: "ac3"},
			want: PlayRemux,
		},
		{
			name: "mkv hevc → reencode",
			info: MediaInfo{Container: "matroska,webm", VideoCodec: "hevc", AudioCodec: "aac"},
			want: PlayReencode,
		},
		{
			name: "avi xvid → reencode",
			info: MediaInfo{Container: "avi", VideoCodec: "mpeg4", AudioCodec: "mp3"},
			want: PlayReencode,
		},
		{
			name: "mkv vp9 → reencode (vp9 can't ride mpegts)",
			info: MediaInfo{Container: "matroska,webm", VideoCodec: "vp9", AudioCodec: "aac"},
			want: PlayReencode,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecidePlayMode(tc.info); got != tc.want {
				t.Errorf("DecidePlayMode(%+v) = %v, want %v", tc.info, got, tc.want)
			}
		})
	}
}

func TestAudioNeedsTranscode(t *testing.T) {
	for codec, want := range map[string]bool{
		"aac": false, "AAC": false, "mp3": false,
		"ac3": true, "eac3": true, "dts": true, "": true, "flac": true,
	} {
		if got := AudioNeedsTranscode(codec); got != want {
			t.Errorf("AudioNeedsTranscode(%q) = %v, want %v", codec, got, want)
		}
	}
}

func TestPlayModeString(t *testing.T) {
	for m, want := range map[PlayMode]string{
		PlayDirect: "direct", PlayRemux: "remux", PlayReencode: "reencode",
	} {
		if got := m.String(); got != want {
			t.Errorf("PlayMode(%d).String() = %q, want %q", m, got, want)
		}
	}
}
