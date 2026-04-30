// Device authorization grant hooks. The /link page uses
// useApproveDeviceCode to bind the calling user's session to a
// user_code that a separate device (TV app, CLI tool) is polling for.
//
// Backend: internal/api/handlers/auth_device.go.

import { useMutation } from "@tanstack/react-query";
import { api } from "../client";

export function useApproveDeviceCode() {
  return useMutation<{ approved: boolean }, Error, string>({
    mutationFn: (userCode) => api.approveDeviceCode(userCode),
  });
}
