import { FeatureSlide } from "./shared";
import { SCENES } from "./story";

export const Scene05: React.FC<{ durationInFrames: number }> = ({ durationInFrames }) => (
  <FeatureSlide scene={SCENES[4]} index={4} durationInFrames={durationInFrames} />
);
