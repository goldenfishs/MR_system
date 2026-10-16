import { FeatureSlide } from "./shared";
import { SCENES } from "./story";

export const Scene04: React.FC<{ durationInFrames: number }> = ({ durationInFrames }) => (
  <FeatureSlide scene={SCENES[3]} index={3} durationInFrames={durationInFrames} />
);
