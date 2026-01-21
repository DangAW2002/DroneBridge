import { Parameter, Category, ParameterGroup } from './types';

interface ParsedData {
  categories: Category[];
  parameters: Parameter[];
}

export function parseParameterXML(xmlString: string): ParsedData {
  const parser = new DOMParser();
  const xmlDoc = parser.parseFromString(xmlString, 'text/xml');
  
  const groups = xmlDoc.querySelectorAll('group');
  const parameters: Parameter[] = [];
  const groupMap = new Map<string, ParameterGroup>();
  
  groups.forEach(groupEl => {
    const groupName = groupEl.getAttribute('name') || 'Unknown';
    const groupId = groupName.toLowerCase().replace(/[^a-z0-9]+/g, '_');
    
    // Add to group map
    if (!groupMap.has(groupId)) {
      groupMap.set(groupId, { id: groupId, name: groupName });
    }
    
    // Parse parameters in this group
    const paramElements = groupEl.querySelectorAll('parameter');
    paramElements.forEach(paramEl => {
      const param = parseParameter(paramEl, groupId);
      if (param) {
        parameters.push(param);
      }
    });
  });
  
  // Create categories based on parameter category attributes (Standard, System, Developer)
  const categories = createCategoriesFromParameters(parameters, Array.from(groupMap.values()));
  
  return { categories, parameters };
}

function parseParameter(paramEl: Element, groupId: string): Parameter | null {
  const name = paramEl.getAttribute('name');
  if (!name) return null;
  
  const typeAttr = paramEl.getAttribute('type') || 'INT32';
  const defaultValue = paramEl.getAttribute('default') || '0';
  const isBoolean = paramEl.getAttribute('boolean') === 'true';
  const isVolatile = paramEl.getAttribute('volatile') === 'true';
  const categoryAttr = paramEl.getAttribute('category');
  
  // Get child elements
  const shortDesc = getElementText(paramEl, 'short_desc') || name;
  const longDesc = getElementText(paramEl, 'long_desc') || shortDesc;
  const unit = getElementText(paramEl, 'unit');
  const minVal = getElementText(paramEl, 'min');
  const maxVal = getElementText(paramEl, 'max');
  const decimalPlaces = getElementText(paramEl, 'decimal');
  const increment = getElementText(paramEl, 'increment');
  
  // Parse enum values if present
  const valuesEl = paramEl.querySelector('values');
  const enumValues: { code: string; description: string }[] = [];
  if (valuesEl) {
    valuesEl.querySelectorAll('value').forEach(valueEl => {
      const code = valueEl.getAttribute('code') || '';
      const description = valueEl.textContent || '';
      enumValues.push({ code, description });
    });
  }
  
  // Parse bitmask if present
  const bitmaskEl = paramEl.querySelector('bitmask');
  const bitmaskValues: { index: string; description: string }[] = [];
  if (bitmaskEl) {
    bitmaskEl.querySelectorAll('bit').forEach(bitEl => {
      const index = bitEl.getAttribute('index') || '';
      const description = bitEl.textContent || '';
      bitmaskValues.push({ index, description });
    });
  }
  
  // Determine parameter type
  let paramType: 'float' | 'int' | 'enum' | 'bool' | 'bitmask' = 'int';
  if (isBoolean) {
    paramType = 'bool';
  } else if (enumValues.length > 0) {
    paramType = 'enum';
  } else if (bitmaskValues.length > 0) {
    paramType = 'bitmask';
  } else if (typeAttr === 'FLOAT') {
    paramType = 'float';
  }
  
  // Parse default value based on type
  let parsedDefault: string | number = defaultValue;
  if (paramType === 'float') {
    parsedDefault = parseFloat(defaultValue) || 0;
  } else if (paramType === 'int' || paramType === 'bitmask') {
    parsedDefault = parseInt(defaultValue, 10) || 0;
  } else if (paramType === 'bool') {
    parsedDefault = parseInt(defaultValue, 10) || 0;
  }
  
  // Check if reboot is required (based on category or volatile attribute)
  const rebootRequired = categoryAttr === 'System' || isVolatile;
  
  const parameter: Parameter = {
    name,
    value: parsedDefault,
    defaultValue: parsedDefault,
    shortDesc,
    longDesc,
    group: groupId,
    type: paramType,
    rebootRequired,
    unit: unit || undefined,
    min: minVal ? parseFloat(minVal) : undefined,
    max: maxVal ? parseFloat(maxVal) : undefined,
    isAdvanced: categoryAttr === 'Developer',
    enumValues: enumValues.length > 0 ? enumValues : undefined,
    bitmaskValues: bitmaskValues.length > 0 ? bitmaskValues : undefined,
    decimalPlaces: decimalPlaces ? parseInt(decimalPlaces, 10) : undefined,
    increment: increment ? parseFloat(increment) : undefined,
    category: categoryAttr || undefined,
    isVolatile,
    xmlType: typeAttr, // Store original XML type for MAVLink communication
  };
  
  return parameter;
}

function getElementText(parent: Element, tagName: string): string | undefined {
  const el = parent.querySelector(tagName);
  return el?.textContent?.trim() || undefined;
}

// Group parameters by their category attribute and then by group
function createCategoriesFromParameters(parameters: Parameter[], groups: ParameterGroup[]): Category[] {
  // Create a map of group id to group for quick lookup
  const groupMap = new Map<string, ParameterGroup>();
  groups.forEach(g => groupMap.set(g.id, g));
  
  // Track which groups have parameters in each category
  const standardGroups = new Set<string>();
  const developerGroups = new Set<string>();
  const systemGroups = new Set<string>();
  
  // Categorize based on parameter category attribute
  parameters.forEach(param => {
    const category = param.category?.toLowerCase();
    if (category === 'developer') {
      developerGroups.add(param.group);
    } else if (category === 'system') {
      systemGroups.add(param.group);
    } else {
      // Standard (no category or unknown category)
      standardGroups.add(param.group);
    }
  });
  
  const categories: Category[] = [];
  
  // Standard category (parameters without category attribute) - show first
  if (standardGroups.size > 0) {
    const sortedGroups = Array.from(standardGroups)
      .map(id => groupMap.get(id)!)
      .filter(Boolean)
      .sort((a, b) => a.name.localeCompare(b.name));
    
    categories.push({
      id: 'standard',
      name: 'Standard',
      groups: sortedGroups,
    });
  }
  
  // System category
  if (systemGroups.size > 0) {
    const sortedGroups = Array.from(systemGroups)
      .map(id => groupMap.get(id)!)
      .filter(Boolean)
      .sort((a, b) => a.name.localeCompare(b.name));
    
    categories.push({
      id: 'system',
      name: 'System',
      groups: sortedGroups,
    });
  }
  
  // Developer category
  if (developerGroups.size > 0) {
    const sortedGroups = Array.from(developerGroups)
      .map(id => groupMap.get(id)!)
      .filter(Boolean)
      .sort((a, b) => a.name.localeCompare(b.name));
    
    categories.push({
      id: 'developer',
      name: 'Developer',
      groups: sortedGroups,
    });
  }
  
  return categories;
}

// Function to load XML from file path (for use in browser)
export async function loadParameterXML(filePath: string): Promise<ParsedData> {
  const response = await fetch(filePath);
  const xmlString = await response.text();
  return parseParameterXML(xmlString);
}
