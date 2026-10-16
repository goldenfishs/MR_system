import { FeatureSlide } from "./shared";
import { SCENES } from "./story";

export const Scene02: React.FC<{ durationInFrames: number }> = ({ durationInFrames }) => (
  <FeatureSlide scene={SCENES[1]} index={1} durationInFrames={durationInFrames} />
);
