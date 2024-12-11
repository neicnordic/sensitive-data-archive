package no.uio.ifi.localega.doa.model;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import lombok.Getter;
import lombok.RequiredArgsConstructor;
import lombok.Setter;
import lombok.ToString;
import org.hibernate.annotations.CacheConcurrencyStrategy;
import org.hibernate.annotations.Immutable;

@org.hibernate.annotations.Cache(usage = CacheConcurrencyStrategy.TRANSACTIONAL)
@Entity
@Immutable
@Getter
@Setter
@ToString
@RequiredArgsConstructor
@Table(name = "datasets", schema = "sda")
public class Dataset {
    @Id
    private Long id;

    @Column(name = "stable_id", unique = true)
    private String stableId;
}
