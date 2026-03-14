import { useState, useCallback } from "react";
import type { ReactNode } from "react";
import AccountStep from "./AccountStep";
import LibrariesStep from "./LibrariesStep";
import SettingsStep from "./SettingsStep";
import CompleteStep from "./CompleteStep";

// ─── Types ───────────────────────────────────────────────────────────────────

interface AdminData {
  username: string;
  password: string;
  displayName?: string;
}

interface LibraryEntry {
  name: string;
  contentType: string;
  path: string;
}

interface SettingsData {
  tmdbApiKey?: string;
  hwAccel?: string;
}

export interface SetupData {
  user?: AdminData;
  libraries?: LibraryEntry[];
  settings?: SettingsData;
}

// ─── Step Definitions ────────────────────────────────────────────────────────

const STEPS = [
  { label: "Admin Account", number: 1 },
  { label: "Libraries", number: 2 },
  { label: "Settings", number: 3 },
  { label: "Complete", number: 4 },
] as const;

// ─── Step Indicator ──────────────────────────────────────────────────────────

function StepIndicator({ currentStep }: { currentStep: number }) {
  return (
    <div className="flex items-center justify-center gap-0">
      {STEPS.map((step, index) => {
        const isActive = currentStep === index;
        const isCompleted = currentStep > index;

        return (
          <div key={step.number} className="flex items-center">
            {/* Connector line (before, except first) */}
            {index > 0 && (
              <div
                className={[
                  "h-0.5 w-10 sm:w-16 transition-colors duration-300",
                  isCompleted || isActive ? "bg-accent" : "bg-border",
                ].join(" ")}
              />
            )}

            {/* Step circle + label */}
            <div className="flex flex-col items-center gap-1.5">
              <div
                className={[
                  "flex h-9 w-9 items-center justify-center rounded-full text-sm font-semibold transition-all duration-300",
                  isActive
                    ? "bg-accent text-white shadow-md shadow-accent/30 scale-110"
                    : isCompleted
                      ? "bg-accent/20 text-accent"
                      : "bg-bg-elevated text-text-muted border border-border",
                ].join(" ")}
              >
                {isCompleted ? (
                  <svg className="h-4 w-4" viewBox="0 0 20 20" fill="currentColor">
                    <path
                      fillRule="evenodd"
                      d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
                      clipRule="evenodd"
                    />
                  </svg>
                ) : (
                  step.number
                )}
              </div>
              <span
                className={[
                  "text-xs font-medium whitespace-nowrap transition-colors duration-300",
                  isActive
                    ? "text-accent"
                    : isCompleted
                      ? "text-text-secondary"
                      : "text-text-muted",
                ].join(" ")}
              >
                {step.label}
              </span>
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ─── Logo ────────────────────────────────────────────────────────────────────

function HubPlayLogo() {
  return (
    <div className="flex flex-col items-center gap-3">
      <div className="flex h-16 w-16 items-center justify-center rounded-full bg-accent/10">
        <svg
          className="h-8 w-8 text-accent"
          viewBox="0 0 24 24"
          fill="currentColor"
        >
          <path d="M8 5v14l11-7z" />
        </svg>
      </div>
      <div className="text-center">
        <h1 className="text-2xl font-bold text-text-primary">HubPlay</h1>
        <p className="text-sm text-text-muted mt-1">Setup Wizard</p>
      </div>
    </div>
  );
}

// ─── Main Wizard ─────────────────────────────────────────────────────────────

export default function SetupWizard() {
  const [currentStep, setCurrentStep] = useState(0);
  const [setupData, setSetupData] = useState<SetupData>({});

  const goNext = useCallback(() => {
    setCurrentStep((s) => Math.min(s + 1, STEPS.length - 1));
  }, []);

  const goBack = useCallback(() => {
    setCurrentStep((s) => Math.max(s - 1, 0));
  }, []);

  const handleAccountNext = useCallback(
    (data: AdminData) => {
      setSetupData((prev) => ({ ...prev, user: data }));
      goNext();
    },
    [goNext],
  );

  const handleLibrariesNext = useCallback(
    (data: LibraryEntry[]) => {
      setSetupData((prev) => ({ ...prev, libraries: data }));
      goNext();
    },
    [goNext],
  );

  const handleSettingsNext = useCallback(
    (data: SettingsData) => {
      setSetupData((prev) => ({ ...prev, settings: data }));
      goNext();
    },
    [goNext],
  );

  let stepContent: ReactNode;
  switch (currentStep) {
    case 0:
      stepContent = (
        <AccountStep
          onNext={handleAccountNext}
          initialData={setupData.user}
        />
      );
      break;
    case 1:
      stepContent = (
        <LibrariesStep
          onNext={handleLibrariesNext}
          onBack={goBack}
          initialData={setupData.libraries}
        />
      );
      break;
    case 2:
      stepContent = (
        <SettingsStep
          onNext={handleSettingsNext}
          onBack={goBack}
          initialData={setupData.settings}
        />
      );
      break;
    case 3:
      stepContent = <CompleteStep setupData={setupData} />;
      break;
    default:
      stepContent = null;
  }

  return (
    <div className="flex min-h-screen flex-col items-center bg-bg-base px-4 py-8 sm:py-12">
      {/* Logo */}
      <div className="mb-8">
        <HubPlayLogo />
      </div>

      {/* Step Indicator */}
      <div className="mb-8">
        <StepIndicator currentStep={currentStep} />
      </div>

      {/* Step Content Card */}
      <div className="w-full max-w-2xl rounded-[--radius-lg] border border-border bg-bg-card p-6 sm:p-8 shadow-lg">
        {stepContent}
      </div>
    </div>
  );
}
