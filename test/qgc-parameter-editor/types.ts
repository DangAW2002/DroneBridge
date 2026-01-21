export interface Parameter {
  name: string;
  value: string | number;
  defaultValue: string | number;
  unit?: string;
  shortDesc: string;
  longDesc: string;
  group: string;
  min?: number;
  max?: number;
  type: 'float' | 'int' | 'enum' | 'bool' | 'bitmask';
  rebootRequired: boolean;
  isAdvanced?: boolean;
  enumValues?: { code: string; description: string }[];
  bitmaskValues?: { index: string; description: string }[];
  decimalPlaces?: number;
  increment?: number;
  category?: string;
  isVolatile?: boolean;
  xmlType?: string; // Original XML type (INT32, FLOAT, etc.)
}

export interface ParameterGroup {
  id: string;
  name: string;
}

export interface Category {
  id: string;
  name: string;
  groups: ParameterGroup[];
}

// MAVLink parameter type mapping
export const MAV_PARAM_TYPE = {
  UINT8: 'UINT8',
  INT8: 'INT8',
  UINT16: 'UINT16',
  INT16: 'INT16',
  UINT32: 'UINT32',
  INT32: 'INT32',
  UINT64: 'UINT64',
  INT64: 'INT64',
  FLOAT: 'FLOAT',
  REAL32: 'FLOAT',
  REAL64: 'FLOAT',
} as const;

// Map Parameter type to MAVLink type
export function getMAVLinkType(param: Parameter): string {
  if (param.xmlType) {
    return param.xmlType;
  }
  
  switch (param.type) {
    case 'float':
      return 'FLOAT';
    case 'int':
    case 'enum':
    case 'bitmask':
      return 'INT32';
    case 'bool':
      return 'INT32'; // PX4 uses INT32 for booleans
    default:
      return 'INT32';
  }
}

// API types for MAVLink communication
export interface ParamSetRequest {
  paramName: string;
  paramValue: number;
  paramType: string;
}

export interface ParamSetResponse {
  success: boolean;
  message: string;
  paramName: string;
  newValue?: number;
}

export interface ConnectionStatus {
  connected: boolean;
  systemId: number;
  message: string;
}