import type { FC } from "react";
import type { MediaStream } from "@/api/types";
import { Badge } from "@/components/common/Badge";

interface MediaMetaProps {
  streams: MediaStream[];
}

function getResolutionLabel(height: number | null): string | null {
  if (height == null) return null;
  if (height >= 2160) return "4K";
  if (height >= 1440) return "1440p";
  if (height >= 1080) return "1080p";
  if (height >= 720) return "720p";
  if (height >= 480) return "480p";
  return `${height}p`;
}

function getChannelLayout(channels: number | null): string | null {
  if (channels == null) return null;
  if (channels === 8) return "7.1";
  if (channels === 6) return "5.1";
  if (channels === 2) return "Stereo";
  if (channels === 1) return "Mono";
  return `${channels}ch`;
}

const MediaMeta: FC<MediaMetaProps> = ({ streams }) => {
  const videoStream = streams.find((s) => s.type === "video");
  const audioStreams = streams.filter((s) => s.type === "audio");
  const subtitleCount = streams.filter((s) => s.type === "subtitle").length;
  const defaultAudio = audioStreams.find((s) => s.is_default) ?? audioStreams[0];

  const badges: { label: string; key: string }[] = [];

  if (videoStream) {
    const codec = videoStream.codec.toUpperCase();
    const resolution = getResolutionLabel(videoStream.height);
    const hdr = videoStream.hdr_type ? ` ${videoStream.hdr_type}` : "";
    const label = resolution ? `${codec} ${resolution}${hdr}` : `${codec}${hdr}`;
    badges.push({ label, key: "video" });
  }

  if (defaultAudio) {
    const codec = defaultAudio.codec.toUpperCase();
    const channels = getChannelLayout(defaultAudio.channels);
    const label = channels ? `${codec} ${channels}` : codec;
    badges.push({ label, key: "audio" });
  }

  if (subtitleCount > 0) {
    badges.push({
      label: `${subtitleCount} ${subtitleCount === 1 ? "Subtitle" : "Subtitles"}`,
      key: "subtitles",
    });
  }

  if (badges.length === 0) return null;

  return (
    <div className="flex flex-wrap items-center gap-2">
      {badges.map(({ label, key }) => (
        <Badge key={key}>{label}</Badge>
      ))}
    </div>
  );
};

export { MediaMeta };
export type { MediaMetaProps };
