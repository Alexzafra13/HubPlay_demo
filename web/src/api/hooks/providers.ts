// Metadata-provider admin (TMDb / Fanart / OpenSubtitles): list and
// update API keys + priority + status. Self-hosted single-tenant
// surface — only the admin user sees these endpoints.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";

export function useProviders() {
  return useQuery<
    Array<{
      name: string;
      type: string;
      status: string;
      priority: number;
      has_api_key: boolean;
      config?: Record<string, string>;
    }>
  >({
    queryKey: queryKeys.providers,
    queryFn: () => api.getProviders(),
  });
}

export function useUpdateProvider() {
  const queryClient = useQueryClient();
  return useMutation<
    { name: string; status: string; priority: number },
    Error,
    {
      name: string;
      data: {
        api_key?: string;
        status?: string;
        priority?: number;
        config?: Record<string, string>;
      };
    }
  >({
    mutationFn: ({ name, data }) => api.updateProvider(name, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.providers });
    },
  });
}
