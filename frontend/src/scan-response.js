// Expand shared metadata after parsing the much smaller wire response.
export function decodeScan(scan) {
  if (!Array.isArray(scan?.missingGroups)) return scan;
  const { missingGroups, ...result } = scan;
  result.missing = missingGroups.flatMap(group => group.episodes.map(episode => ({ ...group.series, ...episode })));
  return result;
}

export function decodeScanResponse(data) {
  if (data?.missingGroups) return decodeScan(data);
  if (data?.scan) return { ...data, scan: decodeScan(data.scan) };
  if (data?.result?.scan) return { ...data, result: { ...data.result, scan: decodeScan(data.result.scan) } };
  return data;
}
