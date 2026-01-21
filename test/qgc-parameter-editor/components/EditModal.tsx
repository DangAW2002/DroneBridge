import React, { useState, useEffect } from 'react';
import { Parameter, ParamSetResponse } from '../types';
import mavlinkApi from '../mavlinkApi';
import { Send, Loader2, CheckCircle2, XCircle, AlertCircle } from 'lucide-react';

interface EditModalProps {
  parameter: Parameter;
  isOpen: boolean;
  onClose: () => void;
  onSave: (paramName: string, newValue: string | number) => void;
}

type SendStatus = 'idle' | 'sending' | 'success' | 'error';

const EditModal: React.FC<EditModalProps> = ({ parameter, isOpen, onClose, onSave }) => {
  const [value, setValue] = useState<string | number>(parameter.value);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [sendStatus, setSendStatus] = useState<SendStatus>('idle');
  const [sendMessage, setSendMessage] = useState<string>('');

  useEffect(() => {
    setValue(parameter.value);
    setSendStatus('idle');
    setSendMessage('');
  }, [parameter]);

  if (!isOpen) return null;

  const handleSave = () => {
    onSave(parameter.name, value);
    onClose();
  };

  const handleReset = () => {
    setValue(parameter.defaultValue);
  };

  const handleSendToVehicle = async () => {
    setSendStatus('sending');
    setSendMessage('Sending to vehicle...');

    try {
      const numericValue = typeof value === 'string' ? parseFloat(value) : value;
      const response: ParamSetResponse = await mavlinkApi.setParameter(parameter, numericValue);
      
      if (response.success) {
        setSendStatus('success');
        setSendMessage(response.message);
        // Update local value with confirmed value from vehicle
        if (response.newValue !== undefined) {
          setValue(response.newValue);
          onSave(parameter.name, response.newValue);
        }
        // Auto-clear success message after 3 seconds
        setTimeout(() => {
          setSendStatus('idle');
          setSendMessage('');
        }, 3000);
      } else {
        setSendStatus('error');
        setSendMessage(response.message);
      }
    } catch (error) {
      setSendStatus('error');
      setSendMessage(error instanceof Error ? error.message : 'Unknown error');
    }
  };

  const getSendStatusIcon = () => {
    switch (sendStatus) {
      case 'sending':
        return <Loader2 className="animate-spin" size={16} />;
      case 'success':
        return <CheckCircle2 size={16} className="text-green-400" />;
      case 'error':
        return <XCircle size={16} className="text-red-400" />;
      default:
        return <Send size={16} />;
    }
  };

  const getSendStatusColor = () => {
    switch (sendStatus) {
      case 'sending':
        return 'bg-yellow-600 hover:bg-yellow-500';
      case 'success':
        return 'bg-green-600 hover:bg-green-500';
      case 'error':
        return 'bg-red-600 hover:bg-red-500';
      default:
        return 'bg-blue-600 hover:bg-blue-500';
    }
  };

  // Render input based on parameter type
  const renderInput = () => {
    // Boolean type - toggle switch or dropdown
    if (parameter.type === 'bool') {
      return (
        <select
          value={Number(value)}
          onChange={(e) => setValue(parseInt(e.target.value))}
          className="w-48 bg-white text-black px-2 py-1.5 border border-slate-400 focus:outline-none focus:border-teal-500 font-mono"
        >
          <option value={0}>Disabled (0)</option>
          <option value={1}>Enabled (1)</option>
        </select>
      );
    }

    // Enum type - dropdown with options
    if (parameter.type === 'enum' && parameter.enumValues && parameter.enumValues.length > 0) {
      return (
        <select
          value={String(value)}
          onChange={(e) => setValue(parseInt(e.target.value) || e.target.value)}
          className="w-64 bg-white text-black px-2 py-1.5 border border-slate-400 focus:outline-none focus:border-teal-500 font-mono text-sm"
        >
          {parameter.enumValues.map((enumVal) => (
            <option key={enumVal.code} value={enumVal.code}>
              {enumVal.code}: {enumVal.description}
            </option>
          ))}
        </select>
      );
    }

    // Bitmask type - checkboxes
    if (parameter.type === 'bitmask' && parameter.bitmaskValues && parameter.bitmaskValues.length > 0) {
      const currentValue = Number(value);
      return (
        <div className="flex flex-col gap-2 max-h-48 overflow-y-auto">
          {parameter.bitmaskValues.map((bit) => {
            const bitIndex = parseInt(bit.index);
            const bitMask = 1 << bitIndex;
            const isChecked = (currentValue & bitMask) !== 0;
            
            return (
              <label key={bit.index} className="flex items-center gap-2 cursor-pointer text-slate-200">
                <input
                  type="checkbox"
                  checked={isChecked}
                  onChange={(e) => {
                    if (e.target.checked) {
                      setValue(currentValue | bitMask);
                    } else {
                      setValue(currentValue & ~bitMask);
                    }
                  }}
                  className="w-4 h-4 rounded bg-slate-700 border-slate-500 focus:ring-teal-500"
                />
                <span className="text-xs">Bit {bit.index}: {bit.description}</span>
              </label>
            );
          })}
        </div>
      );
    }

    // Number types (int/float) - number input
    return (
      <input
        type="number"
        value={value}
        step={parameter.type === 'float' ? (parameter.increment || 'any') : 1}
        min={parameter.min}
        max={parameter.max}
        onChange={(e) => {
          const val = e.target.value;
          if (parameter.type === 'int') setValue(parseInt(val) || 0);
          else if (parameter.type === 'float') setValue(parseFloat(val) || 0);
          else setValue(val);
        }}
        className="w-32 bg-white text-black px-2 py-1 border border-slate-400 focus:outline-none focus:border-teal-500 text-right font-mono"
      />
    );
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-[1px]">
      <div className="bg-slate-800 border border-slate-600 rounded shadow-2xl w-[600px] max-w-full text-slate-200 flex flex-col font-sans text-sm">
        
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-slate-700 bg-slate-800 rounded-t">
          <h3 className="text-lg font-medium text-slate-100">{parameter.name}</h3>
          <div className="flex gap-2">
            <button 
              onClick={onClose}
              className="px-3 py-1 bg-slate-600 hover:bg-slate-500 text-white rounded transition-colors text-xs font-medium"
            >
              Cancel
            </button>
            <button 
              onClick={handleSave}
              className="px-3 py-1 bg-teal-600 hover:bg-teal-500 text-white rounded transition-colors text-xs font-medium"
            >
              Save Local
            </button>
            <button 
              onClick={handleSendToVehicle}
              disabled={sendStatus === 'sending'}
              className={`px-3 py-1 text-white rounded transition-colors text-xs font-medium flex items-center gap-1.5 ${getSendStatusColor()} disabled:opacity-50 disabled:cursor-not-allowed`}
            >
              {getSendStatusIcon()}
              Send to Vehicle
            </button>
          </div>
        </div>

        {/* Content Body */}
        <div className="p-6 flex flex-col gap-6">
          
          {/* Input Row */}
          <div className="flex items-start gap-3">
            <div className="relative">
              {renderInput()}
            </div>
            {parameter.unit && parameter.type !== 'bitmask' && (
              <span className="text-slate-400 mt-1">{parameter.unit}</span>
            )}
            
            <button 
              onClick={handleReset}
              className="ml-auto px-3 py-1.5 bg-slate-600 hover:bg-slate-500 text-slate-200 rounded text-xs transition-colors"
            >
              Reset To Default
            </button>
          </div>

          {/* Bitmask current value display */}
          {parameter.type === 'bitmask' && (
            <div className="text-slate-400 text-xs">
              Current value: {value} (binary: {Number(value).toString(2).padStart(16, '0')})
            </div>
          )}

          {/* Description Block */}
          <div className="text-slate-300 space-y-3">
            <p>{parameter.longDesc}</p>
            {parameter.shortDesc !== parameter.longDesc && (
              <p className="text-slate-400 italic">{parameter.shortDesc}</p>
            )}
            
            <div className="mt-4 space-y-1 text-slate-400">
               {parameter.min !== undefined && <p>Min: {parameter.min} Max: {parameter.max} Default: {parameter.defaultValue}</p>}
               {parameter.rebootRequired && <p className="text-orange-400">Vehicle reboot required after change</p>}
            </div>

            {/* Warning Box */}
            <div className="mt-4 p-3 bg-slate-900/50 border-l-4 border-yellow-600 text-slate-300 text-xs leading-relaxed">
              Warning: Modifying values while vehicle is in flight can lead to vehicle instability and possible vehicle loss. Make sure you know what you are doing and double-check your values before Save!
            </div>

            {/* MAVLink Send Status */}
            {sendMessage && (
              <div className={`mt-4 p-3 border-l-4 text-xs leading-relaxed flex items-center gap-2 ${
                sendStatus === 'success' 
                  ? 'bg-green-900/30 border-green-500 text-green-300'
                  : sendStatus === 'error'
                  ? 'bg-red-900/30 border-red-500 text-red-300'
                  : 'bg-blue-900/30 border-blue-500 text-blue-300'
              }`}>
                {sendStatus === 'sending' && <Loader2 className="animate-spin" size={14} />}
                {sendStatus === 'success' && <CheckCircle2 size={14} />}
                {sendStatus === 'error' && <AlertCircle size={14} />}
                <span>{sendMessage}</span>
              </div>
            )}
          </div>
          
          {/* Footer Checkbox */}
          <div className="flex items-center gap-2 mt-2">
            <input 
              type="checkbox" 
              id="advanced"
              checked={showAdvanced}
              onChange={(e) => setShowAdvanced(e.target.checked)}
              className="w-4 h-4 rounded bg-slate-700 border-slate-500 focus:ring-teal-500" 
            />
            <label htmlFor="advanced" className="text-slate-300 select-none cursor-pointer">Advanced settings</label>
          </div>

        </div>
      </div>
    </div>
  );
};

export default EditModal;
