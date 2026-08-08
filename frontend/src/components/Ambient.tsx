export function AmbientBackground() {
  return (
    <div
      className="pointer-events-none absolute inset-0"
      style={{
        backgroundImage:
          'radial-gradient(600px 400px at 8% 0%, rgba(10,132,255,0.07), transparent 60%),' +
          'radial-gradient(700px 500px at 100% 100%, rgba(255,69,58,0.04), transparent 60%),' +
          'repeating-linear-gradient(0deg, rgba(142,142,147,0.025) 0 1px, transparent 1px 48px),' +
          'repeating-linear-gradient(90deg, rgba(142,142,147,0.025) 0 1px, transparent 1px 48px)',
      }}
    />
  );
}
