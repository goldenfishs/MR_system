import { Fragment } from "react";
import { TransitionSeries, linearTiming } from "@remotion/transitions";
import { fade } from "@remotion/transitions/fade";
import { slide } from "@remotion/transitions/slide";
import type { Caption } from "@remotion/captions";
import { Composition, interpolate, useCurrentFrame, useVideoConfig } from "remotion";
import captionsJson from "../public/captions.json";
import timingJson from "./scenes/generated-timing.json";
import { SCENES } from "./scenes/story";
import {
  Scene01,
  Scene02,
  Scene03,
  Scene04,
  Scene05,
  Scene06,
  Scene07,
  Scene08,
} from "./scenes";

const CAPTIONS = captionsJson as Caption[];
const TIMING = timingJson as {
  fps: number;
  transitionFrames: number;
  durationInFrames: number;
  scenes: Array<{ id: string; startFrame: number; durationInFrames: number }>;
};

const SCENE_COMPONENTS = [
  Scene01,
  Scene02,
  Scene03,
  Scene04,
  Scene05,
  Scene06,
  Scene07,
  Scene08,
];

const CaptionTrack: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const timeMs = (frame / fps) * 1000;
  const caption = CAPTIONS.find((item) => timeMs >= item.startMs && timeMs <= item.endMs);
  if (!caption) return null;

  const relativeFrame = Math.round(((timeMs - caption.startMs) / 1000) * fps);
  const displayText = formatCaption(caption.text);
  return (
    <div
      style={{
        position: "absolute",
        left: 0,
        right: 0,
        bottom: 83,
        display: "flex",
        justifyContent: "center",
        zIndex: 20,
        opacity: interpolate(relativeFrame, [0, 7], [0, 1], {
          extrapolateLeft: "clamp",
          extrapolateRight: "clamp",
        }),
      }}
    >
      <div
        style={{
          maxWidth: 1590,
          padding: "18px 32px",
          border: "1px solid rgba(210,229,240,.16)",
          borderRadius: 16,
          background: "rgba(7, 13, 24, .86)",
          boxShadow: "0 12px 40px rgba(0,0,0,.24)",
          color: "#f4f7fb",
          textAlign: "center",
          fontFamily: "-apple-system, BlinkMacSystemFont, 'PingFang SC', sans-serif",
          fontSize: 31,
          fontWeight: 500,
          lineHeight: 1.42,
          letterSpacing: 0.4,
        }}
      >
        {displayText}
      </div>
    </div>
  );
};

const formatCaption = (text: string) => {
  const middle = Math.floor(text.length / 2);
  const boundaries = [...text].flatMap((character, index) =>
    "，。；：、！？".includes(character) ? [index + 1] : [],
  );
  const candidates = boundaries.filter((point) => point >= 14 && point <= text.length - 14);
  if (candidates.length === 0) return text;
  const splitAt = candidates.reduce((best, point) =>
    Math.abs(point - middle) < Math.abs(best - middle) ? point : best,
  );
  return `${text.slice(0, splitAt)}\n${text.slice(splitAt)}`;
};

const FeatureIntro: React.FC = () => (
  <>
    <TransitionSeries>
      {SCENE_COMPONENTS.map((SceneComponent, index) => (
        <Fragment key={SCENES[index].id}>
          <TransitionSeries.Sequence
            durationInFrames={TIMING.scenes[index].durationInFrames}
            name={SCENES[index].title.replace("\n", " ")}
          >
            <SceneComponent durationInFrames={TIMING.scenes[index].durationInFrames} />
          </TransitionSeries.Sequence>
          {index < SCENE_COMPONENTS.length - 1 ? (
            <TransitionSeries.Transition
              presentation={
                index === 1 ? slide({ direction: "from-right" }) : fade()
              }
              timing={linearTiming({ durationInFrames: TIMING.transitionFrames })}
            />
          ) : null}
        </Fragment>
      ))}
    </TransitionSeries>
    <CaptionTrack />
  </>
);

export const MyComposition: React.FC = () => (
  <Composition
    id="FeatureIntro"
    component={FeatureIntro}
    durationInFrames={TIMING.durationInFrames}
    fps={TIMING.fps}
    width={1920}
    height={1080}
  />
);
