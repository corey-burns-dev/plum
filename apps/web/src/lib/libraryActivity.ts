export type LibraryActivity = "importing" | "finishing" | "identifying";

export function getLibraryActivity(options: {
  scanPhase?: string;
  enriching?: boolean;
  identifyPhase?: string;
  localIdentifyPhase?: string;
}): LibraryActivity | undefined {
  const backendIdentifying =
    options.identifyPhase === "queued" || options.identifyPhase === "identifying";
  const localIdentifying =
    options.localIdentifyPhase === "queued" ||
    options.localIdentifyPhase === "identifying" ||
    options.localIdentifyPhase === "soft-reveal";

  if (backendIdentifying || localIdentifying) {
    return "identifying";
  }
  if (options.scanPhase === "queued" || options.scanPhase === "scanning") {
    return "importing";
  }
  if (options.scanPhase === "completed" && options.enriching) {
    return "finishing";
  }
  return undefined;
}

export function getLibraryActivityLabel(activity: LibraryActivity | undefined): string | undefined {
  switch (activity) {
    case "importing":
      return "Importing";
    case "finishing":
      return "Finishing";
    case "identifying":
      return "Identifying";
    default:
      return undefined;
  }
}
