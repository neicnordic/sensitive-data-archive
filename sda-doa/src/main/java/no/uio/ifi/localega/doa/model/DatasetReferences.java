package no.uio.ifi.localega.doa.model;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import lombok.*;
import org.hibernate.annotations.CacheConcurrencyStrategy;
import org.hibernate.annotations.Immutable;

@org.hibernate.annotations.Cache(usage = CacheConcurrencyStrategy.TRANSACTIONAL)
@Entity
@Immutable
@Getter
@Setter
@ToString
@RequiredArgsConstructor
@Table(name = "dataset_references", schema = "sda")
public class DatasetReferences {
    @Id
    private Integer id;

    @Column(name = "dataset_id", nullable = false)
    private Integer datasetId;

    @Column(name = "reference_id", nullable = false)
    private String referenceId;
}
