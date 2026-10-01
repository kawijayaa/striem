import type { FieldMetadata, FieldsResponse, SchemaResponse } from '../types';

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function isField(value: unknown): value is FieldMetadata {
  return isRecord(value) && typeof value.path === 'string' && typeof value.type === 'string';
}

export function parseFieldsResponse(value: unknown): FieldsResponse {
  if (!isRecord(value) || !Array.isArray(value.common) || !value.common.every(isField)
    || !Array.isArray(value.tables) || !value.tables.every(group => isRecord(group)
      && typeof group.table === 'string' && Array.isArray(group.fields) && group.fields.every(isField))) {
    throw new Error('Invalid field catalog response');
  }
  return value as unknown as FieldsResponse;
}

export function parseSchemaResponse(value: unknown): SchemaResponse {
  if (!isRecord(value)
    || (value.challengeName !== undefined && typeof value.challengeName !== 'string')
    || (value.tables !== undefined && (!Array.isArray(value.tables) || !value.tables.every(table => isRecord(table)
      && typeof table.name === 'string' && typeof table.description === 'string')))
    || (value.functions !== undefined && (!Array.isArray(value.functions) || !value.functions.every(name => typeof name === 'string')))) {
    throw new Error('Invalid schema response');
  }
  return value as SchemaResponse;
}
