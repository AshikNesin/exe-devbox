import React from 'react';
import { AbsoluteFill, interpolate, spring, useCurrentFrame, useVideoConfig } from 'remotion';

const blue = '#1683ff';
const navy = '#081b35';
const muted = '#7890aa';
const green = '#39d98a';

const commands = [
  { at: 0, text: 'devbox setup --default-domain example.com', label: 'SETUP', lines: ['Installing Node.js, portless, and nginx...', '✓ VM identity discovered', '✓ nginx + portless configured'] },
  { at: 92, text: 'devbox new myapp', label: 'ONBOARD', lines: ['DNS: myapp.example.com → CNAME', '✓ Domain registered with exe.dev', '✓ nginx route written and reloaded'] },
  { at: 190, text: 'devbox dev myapp', label: 'DEVELOP', lines: ['VITE_HMR_URL=wss://myapp.example.com', '✓ portless route registered', '🌐 https://myapp.example.com'] },
  { at: 288, text: 'devbox remove myapp --yes', label: 'REMOVE', lines: ['✓ DNS CNAME removed', '✓ exe.dev domain unregistered', '✓ nginx route removed — codebase untouched'] },
];

const Icon = ({ type }: { type: string }) => {
  const glyph = type === 'REMOVE' ? '−' : type === 'DEVELOP' ? '↗' : type === 'ONBOARD' ? '＋' : '◆';
  return <div style={{ width: 58, height: 58, borderRadius: 16, background: type === 'REMOVE' ? '#ffebeb' : '#e5f1ff', color: type === 'REMOVE' ? '#e65353' : blue, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 30, fontWeight: 800 }}>{glyph}</div>;
};

export const DevboxDemo: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const intro = spring({ frame, fps, config: { damping: 14, stiffness: 90 } });
  const activeIndex = Math.min(commands.length - 1, Math.max(0, Math.floor(frame / 96)));
  const active = commands[activeIndex];
  const local = frame - active.at;
  const terminalProgress = interpolate(local, [0, 18], [0, 1], { extrapolateRight: 'clamp' });
  const commandOpacity = interpolate(local, [0, 8, 80, 94], [0, 1, 1, 0], { extrapolateLeft: 'clamp', extrapolateRight: 'clamp' });
  const cardY = interpolate(intro, [0, 1], [35, 0]);

  return (
    <AbsoluteFill style={{ background: '#f7fbff', fontFamily: 'Inter, Arial, sans-serif', color: navy }}>
      <div style={{ position: 'absolute', inset: 0, background: 'radial-gradient(circle at 75% 10%, #d8edff 0, transparent 36%), linear-gradient(120deg, #fafdff 0%, #eef7ff 100%)' }} />
      <div style={{ position: 'relative', zIndex: 1, padding: '82px 112px', height: '100%', boxSizing: 'border-box' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 18, opacity: intro }}>
          <div style={{ width: 54, height: 54, borderRadius: 15, background: blue, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'white', fontSize: 28 }}>📦</div>
          <div style={{ fontSize: 30, fontWeight: 800, letterSpacing: -1 }}>devbox</div>
          <div style={{ marginLeft: 'auto', color: muted, fontSize: 19 }}>multi-project development on exe.dev</div>
        </div>

        <div style={{ display: 'flex', gap: 76, marginTop: 95, transform: `translateY(${cardY}px)` }}>
          <div style={{ width: 690 }}>
            <div style={{ color: blue, fontSize: 20, fontWeight: 800, letterSpacing: 2, marginBottom: 24 }}>ONE VM. EVERY PROJECT.</div>
            <div style={{ fontSize: 72, lineHeight: 1.04, fontWeight: 850, letterSpacing: -3 }}>Your instant<br /><span style={{ color: blue }}>cloud devbox.</span></div>
            <div style={{ fontSize: 26, lineHeight: 1.45, color: '#536b84', marginTop: 28, maxWidth: 620 }}>Wire DNS, exe.dev domains, nginx, portless, and Vite HMR in seconds — without touching your codebase.</div>
            <div style={{ display: 'flex', gap: 16, marginTop: 48 }}>
              {['setup', 'new', 'dev', 'remove'].map((x) => <div key={x} style={{ padding: '12px 18px', borderRadius: 99, background: '#fff', border: '1px solid #d7e6f4', color: x === 'remove' ? '#e65353' : '#45617d', fontFamily: 'monospace', fontSize: 18 }}>devbox {x}</div>)}
            </div>
          </div>

          <div style={{ flex: 1, paddingTop: 12 }}>
            <div style={{ background: '#0c1420', borderRadius: 22, boxShadow: '0 28px 80px rgba(11,47,83,.22)', overflow: 'hidden' }}>
              <div style={{ height: 54, display: 'flex', alignItems: 'center', padding: '0 22px', gap: 9, borderBottom: '1px solid #273447' }}>
                {['#ff5f57', '#ffbd2e', '#28c840'].map((c) => <div key={c} style={{ width: 13, height: 13, borderRadius: '50%', background: c }} />)}
                <div style={{ color: '#8291a4', marginLeft: 12, fontFamily: 'monospace', fontSize: 16 }}>devbox — terminal</div>
              </div>
              <div style={{ padding: '30px 34px 34px', height: 445, boxSizing: 'border-box', fontFamily: 'monospace', fontSize: 20, lineHeight: 1.7 }}>
                <div style={{ color: '#9badc0', marginBottom: 20 }}>$ <span style={{ color: '#fff' }}>{active.text}</span></div>
                <div style={{ opacity: commandOpacity }}>
                  {active.lines.map((line, i) => <div key={line} style={{ color: line.startsWith('✓') || line.startsWith('🌐') ? green : '#a9bdd2', opacity: interpolate(terminalProgress, [i * .2, Math.min(1, i * .2 + .25)], [0, 1], { extrapolateLeft: 'clamp', extrapolateRight: 'clamp' }) }}>{line}</div>)}
                </div>
                <div style={{ marginTop: 26, color: '#5c7188', opacity: commandOpacity }}>// {active.label.toLowerCase()} complete</div>
              </div>
            </div>
          </div>
        </div>

        <div style={{ position: 'absolute', left: 112, right: 112, bottom: 80, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <div style={{ color: muted, fontSize: 20 }}>A clean path from fresh VM to public dev environment.</div>
          <div style={{ display: 'flex', gap: 18 }}>
            {commands.map((c, i) => <div key={c.label} style={{ display: 'flex', alignItems: 'center', gap: 12, opacity: i === activeIndex ? 1 : .45 }}><Icon type={c.label} /><span style={{ fontSize: 17, fontWeight: 750, color: i === activeIndex ? navy : muted }}>{c.label}</span></div>)}
          </div>
        </div>
      </div>
    </AbsoluteFill>
  );
};
