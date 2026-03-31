import { useState, useCallback } from "react";
import type { FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useSetupCreateLibraries } from "@/api/hooks";
import { Button, Input } from "@/components/common";
import { FolderBrowser } from "@/components/setup/FolderBrowser";

// ─── Types ───────────────────────────────────────────────────────────────────

interface LibraryEntry {
  name: string;
  contentType: string;
  path: string;
}

interface LibrariesStepProps {
  onNext: (data: LibraryEntry[]) => void;
  onBack: () => void;
  initialData?: LibraryEntry[];
}

const CONTENT_TYPE_KEYS = [
  { value: "movies", key: "movies" },
  { value: "tvshows", key: "tvShows" },
  { value: "livetv", key: "liveTv" },
] as const;

function createEmptyEntry(): LibraryEntry {
  return { name: "", contentType: "movies", path: "" };
}

// ─── Component ───────────────────────────────────────────────────────────────

export default function LibrariesStep({
  onNext,
  onBack,
  initialData,
}: LibrariesStepProps) {
  const { t } = useTranslation();
  const createLibraries = useSetupCreateLibraries();

  const [libraries, setLibraries] = useState<LibraryEntry[]>(
    initialData && initialData.length > 0
      ? initialData
      : [createEmptyEntry()],
  );
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [serverError, setServerError] = useState<string | null>(null);
  const [browseIndex, setBrowseIndex] = useState<number | null>(null);

  const updateEntry = useCallback(
    (index: number, field: keyof LibraryEntry, value: string) => {
      setLibraries((prev) =>
        prev.map((entry, i) =>
          i === index ? { ...entry, [field]: value } : entry,
        ),
      );
      // Clear field-specific error when user edits
      setErrors((prev) => {
        const copy = { ...prev };
        delete copy[`${index}.${field}`];
        return copy;
      });
    },
    [],
  );

  const addEntry = useCallback(() => {
    setLibraries((prev) => [...prev, createEmptyEntry()]);
  }, []);

  const removeEntry = useCallback((index: number) => {
    setLibraries((prev) => {
      if (prev.length <= 1) return prev;
      return prev.filter((_, i) => i !== index);
    });
    // Clear errors for removed entry
    setErrors((prev) => {
      const copy: Record<string, string> = {};
      for (const [key, val] of Object.entries(prev)) {
        const entryIdx = parseInt(key.split(".")[0], 10);
        if (entryIdx !== index) {
          copy[key] = val;
        }
      }
      return copy;
    });
  }, []);

  const handleBrowseSelect = useCallback(
    (path: string) => {
      if (browseIndex !== null) {
        updateEntry(browseIndex, "path", path);
        // Auto-fill name from the last path segment if name is empty
        setLibraries((prev) =>
          prev.map((entry, i) => {
            if (i === browseIndex && !entry.name.trim()) {
              const segments = path.split("/").filter(Boolean);
              const folderName = segments[segments.length - 1] ?? "";
              return { ...entry, path, name: folderName };
            }
            return i === browseIndex ? { ...entry, path } : entry;
          }),
        );
        setBrowseIndex(null);
      }
    },
    [browseIndex, updateEntry],
  );

  function validate(): boolean {
    const newErrors: Record<string, string> = {};
    const filledEntries = libraries.filter(
      (e) => e.name.trim() || e.path.trim(),
    );

    // If nothing filled, that's okay — user can skip
    if (filledEntries.length === 0) return true;

    for (let i = 0; i < libraries.length; i++) {
      const entry = libraries[i];
      const hasAnyInput = entry.name.trim() || entry.path.trim();

      if (!hasAnyInput) continue; // skip completely empty rows

      if (!entry.name.trim()) {
        newErrors[`${i}.name`] = "Library name is required";
      }

      if (!entry.path.trim()) {
        newErrors[`${i}.path`] = "Path is required";
      }
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setServerError(null);

    if (!validate()) return;

    const filledLibraries = libraries.filter(
      (entry) => entry.name.trim() && entry.path.trim(),
    );

    // If no libraries, skip to next step
    if (filledLibraries.length === 0) {
      onNext([]);
      return;
    }

    const payload = filledLibraries.map((entry) => ({
      name: entry.name.trim(),
      content_type: entry.contentType,
      paths: [entry.path.trim()],
    }));

    createLibraries.mutate(payload, {
      onSuccess() {
        onNext(filledLibraries);
      },
      onError(err) {
        setServerError(
          err.message || "Failed to create libraries. Please try again.",
        );
      },
    });
  }

  function handleSkip() {
    onNext([]);
  }

  return (
    <div>
      <div className="mb-6">
        <h2 className="text-xl font-semibold text-text-primary">
          {t("setup.libraries.title")}
        </h2>
        <p className="mt-1 text-sm text-text-secondary">
          {t("setup.libraries.description")}
        </p>
      </div>

      <form onSubmit={handleSubmit} className="flex flex-col gap-6">
        {/* Library entries */}
        <div className="flex flex-col gap-4">
          {libraries.map((entry, index) => (
            <div
              key={index}
              className="relative rounded-[--radius-md] border border-border bg-bg-surface p-4"
            >
              {/* Remove button */}
              {libraries.length > 1 && (
                <button
                  type="button"
                  onClick={() => removeEntry(index)}
                  className="absolute top-3 right-3 p-1 rounded-[--radius-sm] text-text-muted hover:text-error hover:bg-error/10 transition-colors cursor-pointer"
                  aria-label={t("setup.libraries.removeLibrary", { index: index + 1 })}
                >
                  <svg
                    className="h-4 w-4"
                    viewBox="0 0 20 20"
                    fill="currentColor"
                  >
                    <path d="M6.28 5.22a.75.75 0 00-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 101.06 1.06L10 11.06l3.72 3.72a.75.75 0 101.06-1.06L11.06 10l3.72-3.72a.75.75 0 00-1.06-1.06L10 8.94 6.28 5.22z" />
                  </svg>
                </button>
              )}

              <div className="flex flex-col gap-3">
                {/* Row 1: Name + Content Type */}
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <Input
                    label={t("setup.libraries.libraryName")}
                    type="text"
                    value={entry.name}
                    onChange={(e) =>
                      updateEntry(index, "name", e.target.value)
                    }
                    placeholder={t("setup.libraries.namePlaceholder")}
                    error={errors[`${index}.name`]}
                  />

                  <div className="flex flex-col gap-1.5">
                    <label
                      htmlFor={`content-type-${index}`}
                      className="text-sm font-medium text-text-secondary"
                    >
                      {t("setup.libraries.contentType")}
                    </label>
                    <select
                      id={`content-type-${index}`}
                      value={entry.contentType}
                      onChange={(e) =>
                        updateEntry(index, "contentType", e.target.value)
                      }
                      className={[
                        "w-full rounded-[--radius-md] bg-bg-card border border-border px-3 py-2 text-sm",
                        "text-text-primary",
                        "transition-colors duration-150",
                        "focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/30",
                        "cursor-pointer",
                      ].join(" ")}
                    >
                      {CONTENT_TYPE_KEYS.map((ct) => (
                        <option key={ct.value} value={ct.value}>
                          {t(`setup.libraries.${ct.key}`)}
                        </option>
                      ))}
                    </select>
                  </div>
                </div>

                {/* Row 2: Path + Browse */}
                <div className="flex gap-2">
                  <div className="flex-1">
                    <Input
                      label={t("setup.libraries.mediaPath")}
                      type="text"
                      value={entry.path}
                      onChange={(e) =>
                        updateEntry(index, "path", e.target.value)
                      }
                      placeholder={t("setup.libraries.pathPlaceholder")}
                      error={errors[`${index}.path`]}
                    />
                  </div>
                  <div className="flex items-end">
                    <Button
                      type="button"
                      variant="secondary"
                      onClick={() => setBrowseIndex(index)}
                    >
                      <svg
                        className="h-4 w-4"
                        viewBox="0 0 20 20"
                        fill="currentColor"
                      >
                        <path d="M3.75 3A1.75 1.75 0 002 4.75v3.26a3.235 3.235 0 011.75-.51h12.5c.644 0 1.245.188 1.75.51V6.75A1.75 1.75 0 0016.25 5h-4.836a.25.25 0 01-.177-.073L9.823 3.513A1.75 1.75 0 008.586 3H3.75zM3.75 9A1.75 1.75 0 002 10.75v4.5c0 .966.784 1.75 1.75 1.75h12.5A1.75 1.75 0 0018 15.25v-4.5A1.75 1.75 0 0016.25 9H3.75z" />
                      </svg>
                      {t("common.browse")}
                    </Button>
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>

        {/* Add library button */}
        <button
          type="button"
          onClick={addEntry}
          className="flex items-center justify-center gap-2 rounded-[--radius-md] border border-dashed border-border py-3 text-sm text-text-secondary hover:border-accent hover:text-accent transition-colors cursor-pointer"
        >
          <svg className="h-4 w-4" viewBox="0 0 20 20" fill="currentColor">
            <path d="M10.75 4.75a.75.75 0 00-1.5 0v4.5h-4.5a.75.75 0 000 1.5h4.5v4.5a.75.75 0 001.5 0v-4.5h4.5a.75.75 0 000-1.5h-4.5v-4.5z" />
          </svg>
          {t("setup.libraries.addAnother")}
        </button>

        {serverError && (
          <p className="rounded-[--radius-sm] bg-error/10 px-3 py-2 text-sm text-error">
            {serverError}
          </p>
        )}

        {/* Navigation buttons */}
        <div className="flex items-center justify-between pt-2">
          <Button type="button" variant="ghost" onClick={onBack}>
            {t("common.back")}
          </Button>

          <div className="flex items-center gap-3">
            <Button type="button" variant="ghost" onClick={handleSkip}>
              {t("common.skip")}
            </Button>
            <Button
              type="submit"
              size="lg"
              isLoading={createLibraries.isPending}
            >
              {t("setup.libraries.saveAndContinue")}
            </Button>
          </div>
        </div>
      </form>

      {/* Folder Browser Modal */}
      <FolderBrowser
        isOpen={browseIndex !== null}
        onClose={() => setBrowseIndex(null)}
        onSelect={handleBrowseSelect}
      />
    </div>
  );
}
