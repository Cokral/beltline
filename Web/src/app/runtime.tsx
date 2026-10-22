import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createContext, useContext, useEffect, useMemo, useState, useSyncExternalStore, type PropsWithChildren } from "react";
import { apiRequest } from "../api/client";
import { EventConnectionManager, type ConnectionStatus } from "../api/eventConnection";
import { apiQueryKey } from "../api/resources";
import type { VersionResponse } from "../api/wire.gen";
import { supportedApiVersion } from "../api/events";

interface RuntimeState {
  authRequired: boolean;
  requireSignIn: () => void;
  apiVersion: number | null;
  versionLoading: boolean;
  incompatible: boolean;
  connection: ConnectionStatus;
  canRead: boolean;
  reloadDocument: () => void;
  /** The label for showing a file in the system's file manager, from the system the daemon runs on. */
  revealLabel: string;
}

const RuntimeContext = createContext<RuntimeState | null>(null);

export function RuntimeProvider({ children, reloadDocument }: PropsWithChildren<{ reloadDocument?: () => void }>) {
  const queryClient = useQueryClient();
  const [authRequired, setAuthRequired] = useState(false);
  const [apiVersion, setApiVersion] = useState<number | null>(null);
  const [incompatible, setIncompatible] = useState(false);
  const requireSignIn = () => setAuthRequired(true);
  const onApiVersion = (version: number) => {
    setApiVersion(version);
    if (version !== supportedApiVersion) setIncompatible(true);
  };
  const versionQuery = useQuery({
    queryKey: apiQueryKey("/api/v1/version"),
    queryFn: ({ signal }) => apiRequest<VersionResponse>("/api/v1/version", { signal, onUnauthorized: requireSignIn }),
    retry: false,
    meta: { resourceDeps: [] as string[] }
  });
  useEffect(() => {
    if (versionQuery.data) onApiVersion(versionQuery.data.apiVersion);
  }, [versionQuery.data]);

  const manager = useMemo(() => new EventConnectionManager({
    queryClient,
    onUnauthorized: requireSignIn,
    onApiVersion
  }), [queryClient]);
  useEffect(() => {
    if (authRequired || incompatible) return;
    manager.start();
    return () => manager.stop();
  }, [manager, authRequired, incompatible]);
  const connection = useSyncExternalStore(manager.subscribe, manager.getSnapshot, (): ConnectionStatus => "connecting");

  const value: RuntimeState = {
    authRequired,
    requireSignIn,
    apiVersion,
    versionLoading: versionQuery.isPending,
    incompatible: incompatible || connection === "incompatible",
    connection,
    canRead: versionQuery.isSuccess && !authRequired && !incompatible && connection !== "incompatible",
    reloadDocument: reloadDocument ?? (() => window.location.reload()),
    revealLabel: revealLabelFor(versionQuery.data?.platform)
  };
  return <RuntimeContext.Provider value={value}>{children}</RuntimeContext.Provider>;
}

/** The file manager's name on the daemon's system: Finder on macOS, the plain folder elsewhere. */
export function revealLabelFor(platform: VersionResponse["platform"] | undefined): string {
  return platform === "linux" ? "Show in folder" : "Show in Finder";
}

/** The reveal label, or macOS's outside a RuntimeProvider (as in component tests). */
export function useRevealLabel(): string {
  return useContext(RuntimeContext)?.revealLabel ?? revealLabelFor(undefined);
}

export function useRuntime(): RuntimeState {
  const value = useContext(RuntimeContext);
  if (!value) throw new Error("useRuntime must be used within RuntimeProvider");
  return value;
}
