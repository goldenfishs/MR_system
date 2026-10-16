import { FeatureSlide } from "./shared";
import { SCENES } from "./story";

export const Scene08: React.FC<{ durationInFrames: number }> = ({ durationInFrames }) => (
  <FeatureSlide scene={SCENES[7]} index={7} durationInFrames={durationInFrames} />
);
