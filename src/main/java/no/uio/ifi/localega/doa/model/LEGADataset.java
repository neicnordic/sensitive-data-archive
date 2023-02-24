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
 * Model-POJO for Hibernate/Spring Data, describes LocalEGA dataset.
 */
@org.hibernate.annotations.Cache(usage = CacheConcurrencyStrategy.TRANSACTIONAL)
@Entity
@Immutable
@Table(schema = "local_ega_ebi", name = "file_dataset")
@Data
@EqualsAndHashCode(of = {"fileId", "datasetId"})
@ToString
@RequiredArgsConstructor
public class LEGADataset {

    @Id
    @Size(max = 128)
    @Column(name = "file_id", insertable = false, updatable = false, length = 128)
    private String fileId;

    @Size(max = 128)
    @Column(name = "dataset_id", insertable = false, updatable = false, length = 128)
    private String datasetId;

}
