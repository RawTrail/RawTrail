import { createContext, useContext } from "react";

// The deployment profile (issue #1436): "server" for a fleet, "laptop" for one person watching their own agents. The server reports
// it in the session probe (EDR_PROFILE); the console chooses its defaults from it. It grants and removes nothing: a laptop install
// keeps every page, with the fleet ones a switch away.
export type Profile = "server" | "laptop";

export interface ProfileState {
  readonly profile: Profile;
  // showAdvanced says whether fleet settings (single sign-on, containment, service accounts) are offered in the console's navigation.
  // Always true on a server; a laptop starts without them and the person can turn them on.
  readonly showAdvanced: boolean;
  readonly setShowAdvanced: (show: boolean) => void;
}

export const ProfileContext = createContext<ProfileState>({ profile: "server", showAdvanced: true, setShowAdvanced: () => undefined });

export function useProfile(): ProfileState {
  return useContext(ProfileContext);
}

// ADVANCED_KEY is where a laptop's choice to show advanced settings is kept, in this browser only.
export const ADVANCED_KEY = "rawtrail.showAdvancedSettings";

// toProfile reads the session probe's profile. A server that predates the field sends none, and is a server.
export function toProfile(value: string | undefined): Profile {
  return value === "laptop" ? "laptop" : "server";
}
