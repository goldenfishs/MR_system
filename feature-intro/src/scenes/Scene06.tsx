import { FeatureSlide } from "./shared";
import { SCENES } from "./story";

export const Scene06: React.FC<{ durationInFrames: number }> = ({ durationInFrames }) => (
  <FeatureSlide scene={SCENES[5]} index={5} durationInFrames={durationInFrames} />
);
