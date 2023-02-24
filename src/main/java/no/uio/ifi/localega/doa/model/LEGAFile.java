package no.uio.ifi.localega.doa.model;

import lombok.Data;
import lombok.EqualsAndHashCode;
import lombok.RequiredArgsConstructor;
import lombok.ToString;
import org.hibernate.annotations.CacheConcurrencyStrategy;
import org.hibernate.annotations.Immutable;

import javax.persistence.Column;
import javax.persistence.Entity;
import javax.persistence.Id;
import javax.persistence.Table;
import javax.validation.constraints.Size;

/**
 * Model-POJO for Hibernate/Spring Data, describes LocalEGA file.
 */
@org.hibernate.annotations.Cache(usage = CacheConcurrencyStrategy.TRANSACTIONAL)
@Entity
@Immutable
@Table(schema = "local_ega_ebi", name = "file")
@Data
@EqualsAndHashCode(of = {"fileId"})
@ToString
@RequiredArgsConstructor
public class LEGAFile {

    @Id
    @Size(max = 128)
    @Column(name = "file_id", insertable = false, updatable = false, length = 128)
    private String fileId;

    @Size(max = 256)
    @Column(name = "file_name", insertable = false, updatable = false, length = 256)
    private String fileName;

    @Size(max = 256)
    @Column(name = "file_path", insertable = false, updatable = false, length = 256)
    private String filePath;

    @Size(max = 128)
    @Column(name = "display_file_name", insertable = false, updatable = false, length = 128)
    private String displayFileName;

    // This is the size of the file that is in the archive, the encrypted part of the file
    @Column(name = "file_size", insertable = false, updatable = false)
    private Long fileSize;

    @Size(max = 128)
    @Column(insertable = false, updatable = false, length = 128)
    private String checksum;

    @Size(max = 12)
    @Column(name = "checksum_type", insertable = false, updatable = false, length = 12)
    private String checksumType;

    @Size(max = 128)
    @Column(name = "unencrypted_checksum", insertable = false, updatable = false, length = 128)
    private String unencryptedChecksum;

    @Size(max = 12)
    @Column(name = "unencrypted_checksum_type", insertable = false, updatable = false, length = 12)
    private String unencryptedChecksumType;

    @Column(name = "decrypted_file_size", insertable = false, updatable = false)
    private Long decryptedFileSize;

    @Column(name = "decrypted_file_checksum", insertable = false, updatable = false)
    private String decryptedFileChecksum;

    @Column(name = "decrypted_file_checksum_type", insertable = false, updatable = false)
    private String decryptedFileChecksumType;

    @Size(max = 13)
    @Column(name = "file_status", insertable = false, updatable = false, length = 13)
    private String fileStatus;

    @Column(insertable = false, updatable = false)
    private String header;

}
