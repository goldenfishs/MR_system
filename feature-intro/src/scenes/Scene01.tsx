import { FeatureSlide } from "./shared";
import { SCENES } from "./story";

export const Scene01: React.FC<{ durationInFrames: number }> = ({ durationInFrames }) => (
  <FeatureSlide scene={SCENES[0]} index={0} durationInFrames={durationInFrames} />
);
