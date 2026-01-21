import React, { useState, useMemo, useEffect } from 'react';
import { parseParameterXML } from './parameterParser';
import { Parameter, Category, ConnectionStatus } from './types';
import EditModal from './components/EditModal';
import { Search, ChevronDown, ChevronRight, Loader2, Wifi, WifiOff, RefreshCw } from 'lucide-react';
import PX4ParameterXML from './src/data/PX4ParameterFactMetaData.xml?raw';
import mavlinkApi from './mavlinkApi';

const App: React.FC = () => {
  const [categories, setCategories] = useState<Category[]>([]);
  const [parameters, setParameters] = useState<Parameter[]>([]);
  const [loading, setLoading] = useState(true);
  const [activeCategoryId, setActiveCategoryId] = useState<string>('');
  const [activeGroupId, setActiveGroupId] = useState<string>('');
  const [expandedCategories, setExpandedCategories] = useState<string[]>([]);
  const [searchTerm, setSearchTerm] = useState('');
  const [showModifiedOnly, setShowModifiedOnly] = useState(false);
  const [editingParam, setEditingParam] = useState<Parameter | null>(null);
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>({
    connected: false,
    systemId: 0,
    message: 'Checking connection...'
  });
  const [isRefreshing, setIsRefreshing] = useState(false);

  // Check connection status periodically
  useEffect(() => {
    const checkConnection = async () => {
      const status = await mavlinkApi.getConnectionStatus();
      setConnectionStatus(status);
    };

    // Initial check
    checkConnection();

    // Poll every 3 seconds
    const interval = setInterval(checkConnection, 3000);
    return () => clearInterval(interval);
  }, []);

  const handleRefreshConnection = async () => {
    setIsRefreshing(true);
    const status = await mavlinkApi.getConnectionStatus();
    setConnectionStatus(status);
    setIsRefreshing(false);
  };

  // Load and parse XML on mount
  useEffect(() => {
    try {
      const { categories: parsedCategories, parameters: parsedParams } = parseParameterXML(PX4ParameterXML);
      setCategories(parsedCategories);
      setParameters(parsedParams);
      
      // Set initial active group and expanded category
      if (parsedCategories.length > 0) {
        setExpandedCategories([parsedCategories[0].id]);
        setActiveCategoryId(parsedCategories[0].id);
        if (parsedCategories[0].groups.length > 0) {
          setActiveGroupId(parsedCategories[0].groups[0].id);
        }
      }
    } catch (error) {
      console.error('Failed to parse parameter XML:', error);
    } finally {
      setLoading(false);
    }
  }, []);

  // Filter Logic - filter by category, group, search, and modified status
  const filteredParams = useMemo(() => {
    return parameters.filter(param => {
      // 1. Filter by Category (Standard, System, Developer)
      let matchesCategory = true;
      if (activeCategoryId) {
        const paramCategory = param.category?.toLowerCase() || 'standard';
        if (activeCategoryId === 'standard') {
          matchesCategory = !param.category || param.category.toLowerCase() === 'standard';
        } else if (activeCategoryId === 'developer') {
          matchesCategory = paramCategory === 'developer';
        } else if (activeCategoryId === 'system') {
          matchesCategory = paramCategory === 'system';
        }
      }

      // 2. Filter by Group
      const matchesGroup = param.group === activeGroupId;

      // 3. Filter by Search Term
      const lowerSearch = searchTerm.toLowerCase();
      const matchesSearch = 
        param.name.toLowerCase().includes(lowerSearch) || 
        param.shortDesc.toLowerCase().includes(lowerSearch);

      // 4. Filter by Modified
      const isModified = String(param.value) !== String(param.defaultValue);
      const matchesModified = showModifiedOnly ? isModified : true;

      return matchesCategory && matchesGroup && matchesSearch && matchesModified;
    });
  }, [parameters, activeCategoryId, activeGroupId, searchTerm, showModifiedOnly]);

  const handleParamUpdate = (paramName: string, newValue: string | number) => {
    setParameters(prev => prev.map(p => 
      p.name === paramName ? { ...p, value: newValue } : p
    ));
  };

  const toggleCategory = (catId: string) => {
    setExpandedCategories(prev => 
      prev.includes(catId) ? prev.filter(id => id !== catId) : [...prev, catId]
    );
    setActiveCategoryId(catId);
  };

  const handleGroupSelect = (catId: string, groupId: string) => {
    setActiveCategoryId(catId);
    setActiveGroupId(groupId);
  };

  const getRowStyle = (index: number) => {
    return index % 2 === 0 ? 'bg-transparent' : 'bg-slate-800/50';
  };

  return (
    <div className="flex h-screen w-full bg-[#1e1e1e] overflow-hidden font-sans text-sm">
      
      {/* Sidebar - 2 Level Hierarchy */}
      <div className="w-64 flex-shrink-0 flex flex-col border-r border-slate-700 bg-[#252526] select-none">
        {/* Search could technically go here too, but QGC often puts it on top of right panel. 
            We will just have the category list here. */}
        <div className="flex-1 overflow-y-auto custom-scrollbar">
          {categories.map(category => {
            const isExpanded = expandedCategories.includes(category.id);
            
            return (
              <div key={category.id} className="border-b border-slate-800">
                {/* Level 1: Category Header */}
                <button
                  onClick={() => toggleCategory(category.id)}
                  className="w-full flex items-center justify-between px-3 py-3 text-slate-200 hover:bg-slate-700 transition-colors bg-[#2d2d2d] text-xs font-bold uppercase tracking-wider"
                >
                  <span>{category.name}</span>
                  {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                </button>

                {/* Level 2: Groups */}
                {isExpanded && (
                  <div className="bg-[#252526]">
                    {category.groups.map(group => {
                      const isActive = activeGroupId === group.id && activeCategoryId === category.id;
                      return (
                        <button
                          key={group.id}
                          onClick={() => handleGroupSelect(category.id, group.id)}
                          className={`w-full text-left px-4 py-2.5 transition-colors border-l-4 text-sm ${
                            isActive
                              ? 'bg-[#3a3a3a] border-yellow-500 text-white'
                              : 'border-transparent text-slate-400 hover:bg-slate-700 hover:text-slate-200'
                          }`}
                        >
                          {group.name}
                        </button>
                      );
                    })}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>

      {/* Main Content Area */}
      <div className="flex-1 flex flex-col bg-[#1e1e1e] min-w-0">
        
        {/* Top Toolbar */}
        <div className="flex items-center p-2 gap-2 bg-[#2d2d2d] border-b border-slate-700 shadow-md z-20">
          <div className="flex bg-white rounded-sm overflow-hidden h-7 w-64">
            <div className="flex items-center justify-center px-2 text-slate-500">
               <Search size={14} />
            </div>
             <input 
              type="text" 
              placeholder="Search" 
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="px-1 py-1 w-full text-black outline-none placeholder-slate-400 text-sm"
            />
          </div>
          <button 
            onClick={() => setSearchTerm('')}
            className="h-7 px-3 bg-slate-600 hover:bg-slate-500 text-white rounded-sm text-xs transition-colors"
          >
            Clear
          </button>

          <div className="h-6 w-px bg-slate-600 mx-2"></div>

          <div className="flex items-center gap-2 select-none">
            <input 
              type="checkbox" 
              id="modifiedOnly"
              checked={showModifiedOnly}
              onChange={(e) => setShowModifiedOnly(e.target.checked)}
              className="w-4 h-4 rounded-sm bg-slate-600 border-none focus:ring-0 accent-teal-600"
            />
            <label htmlFor="modifiedOnly" className="text-slate-300 cursor-pointer text-xs">Show modified only</label>
          </div>

          <div className="flex-1"></div>

          {/* Connection Status */}
          <div className={`flex items-center gap-2 px-3 py-1 rounded-sm text-xs ${
            connectionStatus.connected 
              ? 'bg-green-900/50 text-green-300 border border-green-700' 
              : 'bg-red-900/50 text-red-300 border border-red-700'
          }`}>
            {connectionStatus.connected ? (
              <Wifi size={14} className="text-green-400" />
            ) : (
              <WifiOff size={14} className="text-red-400" />
            )}
            <span>{connectionStatus.connected ? `Connected (ID: ${connectionStatus.systemId})` : 'Disconnected'}</span>
            <button 
              onClick={handleRefreshConnection}
              disabled={isRefreshing}
              className="ml-1 p-0.5 hover:bg-slate-600 rounded transition-colors"
              title="Refresh connection"
            >
              <RefreshCw size={12} className={isRefreshing ? 'animate-spin' : ''} />
            </button>
          </div>

          <button className="h-7 px-4 bg-slate-600 hover:bg-slate-500 text-white rounded-sm text-xs font-medium transition-colors">
            Tools
          </button>
        </div>

        {/* Parameter List Table */}
        <div className="flex-1 overflow-y-auto custom-scrollbar p-0 relative">
          {loading ? (
            <div className="flex flex-col items-center justify-center h-full text-slate-400 p-4">
              <Loader2 className="animate-spin mb-2" size={32} />
              <span>Loading parameters...</span>
            </div>
          ) : filteredParams.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-full text-slate-500 italic p-4">
              <span>No parameters found in this group.</span>
              {searchTerm && <span className="text-xs mt-2">Try clearing the search filter.</span>}
            </div>
          ) : (
            <table className="w-full text-left border-collapse">
              <thead className="sticky top-0 bg-[#252526] text-slate-400 text-xs font-medium shadow-sm z-10">
                {/* Optional Header row to make it look more structured, QGC usually has one */}
                <tr>
                   <td className="px-4 py-2 border-b border-slate-700 w-[250px]">Name</td>
                   <td className="px-4 py-2 border-b border-slate-700 w-[150px]">Value</td>
                   <td className="px-4 py-2 border-b border-slate-700">Description</td>
                </tr>
              </thead>
              <tbody>
                {filteredParams.map((param, idx) => {
                  const isModified = String(param.value) !== String(param.defaultValue);
                  
                  return (
                    <tr 
                      key={param.name}
                      onClick={() => setEditingParam(param)}
                      className={`
                        cursor-pointer border-b border-slate-800/50 hover:bg-slate-700/50 transition-colors
                        ${getRowStyle(idx)}
                      `}
                    >
                      <td className="py-2 px-4 w-[250px] align-top">
                        <div className={`font-mono text-xs sm:text-sm ${isModified ? 'text-orange-400' : 'text-slate-200'}`}>
                          {param.name}
                        </div>
                      </td>
                      <td className="py-2 px-4 w-[150px] align-top">
                        <div className={`text-xs sm:text-sm ${isModified ? 'text-orange-400' : 'text-slate-200'}`}>
                          {param.value} <span className="text-slate-500 text-xs">{param.unit}</span>
                        </div>
                      </td>
                      <td className="py-2 px-4 align-top">
                        <div className="text-slate-400 truncate text-xs sm:text-sm">
                          {param.shortDesc}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>

      {/* Edit Modal */}
      {editingParam && (
        <EditModal 
          parameter={editingParam}
          isOpen={!!editingParam}
          onClose={() => setEditingParam(null)}
          onSave={handleParamUpdate}
        />
      )}
    </div>
  );
};

export default App;