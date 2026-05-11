// Device authorization grant hooks. Used by:
//
//   - /link page — useApproveDeviceCode binds the calling user's
//     session to a user_code that a separate device (TV app, CLI
//     tool) is polling for.
//   - /pair page — useStartDeviceCode + usePollDeviceCode drive the
//     in-app pairing UI on a TV/console browser: start a flow,
//     render a QR + user_code, wait on the SSE /events stream for
//     approval, then call /poll once to swap to cookies.
//
// Backend: internal/api/handlers/auth_device.go.

import { useMutation } from "@tanstack/react-query";
import { api } from "../client";
import type { DeviceStartResponse } from "../types";

export function useApproveDeviceCode() {
  return useMutation<{ approved: boolean }, Error, string>({
    mutationFn: (userCode) => api.approveDeviceCode(userCode),
  });
}

export function useStartDeviceCode() {
  return useMutation<DeviceStartResponse, Error, string>({
    mutationFn: (deviceName) => api.startDeviceCode(deviceName),
  });
}

export function usePollDeviceCode() {
  return useMutation<
    { access_token: string; refresh_token: string; expires_at: string },
    Error,
    string
  >({
    mutationFn: (deviceCode) => api.pollDeviceCode(deviceCode),
  });
}
