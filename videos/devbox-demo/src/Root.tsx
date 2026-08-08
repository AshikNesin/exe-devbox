import { Composition } from 'remotion';
import { DevboxDemo } from './DevboxDemo';

export const RemotionRoot = () => (
  <Composition
    id="DevboxDemo"
    component={DevboxDemo}
    durationInFrames={450}
    fps={30}
    width={1920}
    height={1080}
    defaultProps={{}}
  />
);