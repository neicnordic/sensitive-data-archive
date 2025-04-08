package no.uio.ifi.localega.doa.dto;

import lombok.*;

/**
 * POJO describing the request for exporting <code>LEGAFile</code> or <code>LEGADataset</code>.
 */
@ToString
@EqualsAndHashCode
@NoArgsConstructor
@AllArgsConstructor
@Data
public class ExportRequest {

    @ToString.Exclude
    private String jwtToken;
    private String datasetId;
    private String fileId;
    private String publicKey;
    private String startCoordinate;
    private String endCoordinate;

}
