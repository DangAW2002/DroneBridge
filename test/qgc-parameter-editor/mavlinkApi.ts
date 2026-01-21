import { ParamSetRequest, ParamSetResponse, ConnectionStatus, Parameter, getMAVLinkType } from './types';

const API_BASE_URL = import.meta.env.VITE_API_URL || '/api';

/**
 * MAVLink API client for communicating with Pixhawk via Go backend
 */
export const mavlinkApi = {
  /**
   * Set a parameter on the vehicle
   */
  async setParameter(param: Parameter, newValue: number): Promise<ParamSetResponse> {
    const request: ParamSetRequest = {
      paramName: param.name,
      paramValue: newValue,
      paramType: getMAVLinkType(param),
    };

    try {
      const response = await fetch(`${API_BASE_URL}/param/set`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(request),
      });

      if (!response.ok) {
        const errorText = await response.text();
        return {
          success: false,
          message: `HTTP error: ${response.status} - ${errorText}`,
          paramName: param.name,
        };
      }

      return await response.json();
    } catch (error) {
      return {
        success: false,
        message: `Network error: ${error instanceof Error ? error.message : 'Unknown error'}`,
        paramName: param.name,
      };
    }
  },

  /**
   * Get current connection status to the vehicle
   */
  async getConnectionStatus(): Promise<ConnectionStatus> {
    try {
      const response = await fetch(`${API_BASE_URL}/connection`, {
        method: 'GET',
        headers: {
          'Content-Type': 'application/json',
        },
      });

      if (!response.ok) {
        return {
          connected: false,
          systemId: 0,
          message: `HTTP error: ${response.status}`,
        };
      }

      return await response.json();
    } catch (error) {
      return {
        connected: false,
        systemId: 0,
        message: `Backend not available: ${error instanceof Error ? error.message : 'Unknown error'}`,
      };
    }
  },

  /**
   * Check if the backend server is healthy
   */
  async healthCheck(): Promise<boolean> {
    try {
      const response = await fetch(`${API_BASE_URL}/health`, {
        method: 'GET',
      });
      return response.ok;
    } catch {
      return false;
    }
  },
};

export default mavlinkApi;
