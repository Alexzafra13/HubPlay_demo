import { useState, useCallback } from "react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import AccountStep from "./AccountStep";
import DatabaseStep from "./DatabaseStep";
import LibrariesStep from "./LibrariesStep";
import SettingsStep from "./SettingsStep";
import CompleteStep from "./CompleteStep";
import { BrandWordmark } from "@/components/layout/BrandWordmark";

// ─── Types ───────────────────────────────────────────────────────────────────

interface AdminData {
  username: string;
  password: string;
  displayName?: string;
  /** Set when the operator chose the auto-generate path. The
   *  CompleteStep surfaces this once with a copy button so they
   *  can save it to a password manager — same pattern the admin
   *  /users flow uses for created accounts. We never ferry it
   *  outside the wizard's React state. */
  generatedPassword?: string;
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

const STEP_KEYS = [
  { key: "database", number: 1 },
  { key: "adminAccount", number: 2 },
  { key: "libraries", number: 3 },
  { key: "settings", number: 4 },
  { key: "complete", number: 5 },
] as const;

// ─── Step Indicator ──────────────────────────────────────────────────────────

function StepIndicator({ currentStep }: { currentStep: number }) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center justify-center gap-0">
      {STEP_KEYS.map((step, index) => {
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
                {t(`setup.steps.${step.key}`)}
              </span>
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ─── Logo ────────────────────────────────────────────────────────────────────

// HubPlayLogo — kept visually consistent with the Login page and TopBar so
// a new admin sees the same brand mark across their first session.
function HubPlayLogo() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-center gap-2">
      <BrandWordmark height={44} />
      <p className="text-sm text-text-muted">{t('setup.wizardLabel')}</p>
    </div>
  );
}

// ─── Main Wizard ─────────────────────────────────────────────────────────────

// STEP_MAP translates the server's notion of "where the wizard is"
// into our slot index. The server's `current_step` ("account",
// "libraries", "settings", "complete") was minted before the
// Database step existed and is derived purely from state (users
// exist? libraries exist?) — adding a new step for a screen with no
// persisted side-effect would have meant teaching the server how to
// track wizard cursor state, which we don't want.
//
// So the convention is: the Database step has no server cursor.
// Every time the wizard mounts it lands on slot 0 (Database) unless
// the server says otherwise; the operator either configures pg+restart
// or clicks "skip" and the wizard moves to slot 1 (account). After
// a restart triggered by /setup/db, the next mount lands on slot 0
// again — empty Database form, but the test-against-the-new-driver
// is now a no-op (the binary IS the new driver) so a single skip is
// the natural path.
const STEP_MAP: Record<string, number> = {
  account: 1,
  libraries: 2,
  settings: 3,
  complete: 4,
};

interface SetupWizardProps {
  initialStep?: string;
}

export default function SetupWizard({ initialStep }: SetupWizardProps) {
  const [currentStep, setCurrentStep] = useState(
    initialStep ? (STEP_MAP[initialStep] ?? 0) : 0,
  );
  const [setupData, setSetupData] = useState<SetupData>({});

  const goNext = useCallback(() => {
    setCurrentStep((s) => Math.min(s + 1, STEP_KEYS.length - 1));
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
      stepContent = <DatabaseStep onNext={goNext} />;
      break;
    case 1:
      stepContent = (
        <AccountStep
          onNext={handleAccountNext}
          initialData={setupData.user}
        />
      );
      break;
    case 2:
      stepContent = (
        <LibrariesStep
          onNext={handleLibrariesNext}
          onBack={goBack}
          initialData={setupData.libraries}
        />
      );
      break;
    case 3:
      stepContent = (
        <SettingsStep
          onNext={handleSettingsNext}
          onBack={goBack}
          initialData={setupData.settings}
        />
      );
      break;
    case 4:
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
