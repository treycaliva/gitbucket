import { authService } from './authService';

async function request(url, options = {}) {
  const token = await authService.getToken();
  
  const headers = {
    'Content-Type': 'application/json',
    ...options.headers
  };

  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const response = await fetch(url, {
    ...options,
    headers
  });

  if (!response.ok) {
    let errorMsg = 'API request failed';
    try {
      const contentType = response.headers.get('content-type');
      if (contentType && contentType.includes('application/json')) {
        const errBody = await response.json();
        errorMsg = errBody.error || errBody.message || errorMsg;
      } else {
        const textBody = await response.text();
        errorMsg = textBody || errorMsg;
      }
    } catch {
      // ignore parsing error, use generic msg
    }
    throw new Error(errorMsg);
  }

  // Handle binary vs json responses
  const contentType = response.headers.get('content-type');
  if (contentType && contentType.includes('application/json')) {
    return await response.json();
  }
  return response;
}

export const apiClient = {
  get: (url, options) => request(url, { method: 'GET', ...options }),
  post: (url, body, options) => request(url, { method: 'POST', body: JSON.stringify(body), ...options }),
  put: (url, body, options) => request(url, { method: 'PUT', body: JSON.stringify(body), ...options }),
  patch: (url, body, options) => request(url, { method: 'PATCH', body: JSON.stringify(body), ...options }),
  delete: (url, options) => request(url, { method: 'DELETE', ...options })
};
