import type { FC } from "react";

/**
 * SeekTide — el feedback visual de los saltos de ±10s, sello propio de
 * HubPlay. En vez del círculo-ripple de YouTube o el toast de Plex:
 * una "marea" del acento teal de la app que respira desde el borde del
 * lado saltado, tres chevrons que cascadean en la dirección del salto
 * y el total ACUMULADO en numerales tabulares con glow (tres toques
 * rápidos leen "−30 s", no tres animaciones sueltas).
 *
 * `seq` re-monta el bloque entero (key) para reiniciar la coreografía
 * en cada pulso; el padre acumula `totalSeconds` mientras la marea
 * siga viva. Pointer-events: none — es puro feedback, nunca intercepta
 * el gesto siguiente.
 */
interface SeekTideProps {
  dir: "back" | "fwd";
  totalSeconds: number;
  seq: number;
}

const SeekTide: FC<SeekTideProps> = ({ dir, totalSeconds, seq }) => {
  const isBack = dir === "back";
  const chevron = (
    <svg className="size-7" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d={isBack ? "M14.5 5.5 8 12l6.5 6.5" : "M9.5 5.5 16 12l-6.5 6.5"}
        stroke="currentColor"
        strokeWidth={2.4}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );

  return (
    <div
      key={seq}
      data-testid="seek-tide"
      aria-hidden="true"
      className={[
        "pointer-events-none absolute inset-y-0 z-10 flex w-[36%] flex-col items-center justify-center gap-1 overflow-hidden select-none",
        isBack ? "left-0" : "right-0",
      ].join(" ")}
    >
      {/* La marea: banda de luz del acento que entra desde el borde. */}
      <div
        className="absolute inset-0 animate-[hp-tide_750ms_ease-out_forwards]"
        style={{
          background: `linear-gradient(to ${isBack ? "right" : "left"}, var(--color-accent-glow), transparent 78%)`,
        }}
      />

      {/* Chevrons en cascada — la dirección del salto, en accent-light. */}
      <div
        className="relative flex items-center"
        style={
          {
            color: "var(--color-accent-light)",
            filter: "drop-shadow(0 0 10px var(--color-accent-glow))",
            "--hp-chevron-shift": isBack ? "-12px" : "12px",
          } as React.CSSProperties
        }
      >
        {[0, 1, 2].map((i) => (
          <span
            key={i}
            className="-mx-2 inline-flex animate-[hp-chevron_650ms_ease-out_forwards]"
            style={{ animationDelay: `${i * 70}ms`, opacity: 0 }}
          >
            {chevron}
          </span>
        ))}
      </div>

      {/* Total acumulado: numerales tabulares con el glow de la casa. */}
      <span
        className="relative animate-[hp-count_750ms_ease-out_forwards] text-3xl font-bold tabular-nums text-white"
        style={{ textShadow: "0 0 26px var(--color-accent-glow), 0 2px 10px rgba(0,0,0,0.6)" }}
      >
        {isBack ? "−" : "+"}
        {totalSeconds}
        <span className="ml-1 text-lg font-semibold text-white/70">s</span>
      </span>
    </div>
  );
};

export { SeekTide };
