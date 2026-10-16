import { FeatureSlide } from "./shared";
import { SCENES } from "./story";

export const Scene07: React.FC<{ durationInFrames: number }> = ({ durationInFrames }) => (
  <FeatureSlide scene={SCENES[6]} index={6} durationInFrames={durationInFrames} />
);
